//go:build ignore
#include <cstdint>
#include <cstring>
#include <iostream>

#include "fileref.h"
#include "tpropertymap.h"

#include "mpeg/mpegproperties.h"
#include "flac/flacproperties.h"
#include "ogg/vorbis/vorbisproperties.h"
#include "ogg/opus/opusproperties.h"
#include "ogg/speex/speexproperties.h"
#include "mp4/mp4properties.h"
#include "riff/wav/wavproperties.h"
#include "riff/aiff/aiffproperties.h"
#include "ape/apeproperties.h"
#include "wavpack/wavpackproperties.h"
#include "asf/asfproperties.h"
#include "dsf/dsfproperties.h"
#include "dsdiff/dsdiffproperties.h"
#include "trueaudio/trueaudioproperties.h"
#include "mpc/mpcproperties.h"
#include "shorten/shortenproperties.h"

char *to_char_array(const TagLib::String &s) {
  const std::string str = s.to8Bit(true);
  return ::strdup(str.c_str());
}

TagLib::String to_string(const char *s) {
  return TagLib::String(s, TagLib::String::UTF8);
}

__attribute__((export_name("malloc"))) void *exported_malloc(size_t size) {
  return malloc(size);
}

__attribute__((export_name("taglib_file_tags"))) char **
taglib_file_tags(const char *filename) {
  TagLib::FileRef file(filename);
  if (file.isNull())
    return nullptr;

  auto properties = file.properties();

  size_t len = 0;
  for (const auto &kvs : properties)
    len += kvs.second.size();

  char **tags = static_cast<char **>(malloc(sizeof(char *) * (len + 1)));
  if (!tags)
    return nullptr;

  size_t i = 0;
  for (const auto &kvs : properties)
    for (const auto &v : kvs.second) {
      TagLib::String row = kvs.first + "\t" + v;
      tags[i] = to_char_array(row);
      i++;
    }
  tags[len] = nullptr;

  return tags;
}

// unsupported data: metadata that TagLib's PropertyMap cannot represent as
// text key/values (ID3v2 PRIV/GEOB/POPM, unknown or binary frames, ...).
// descriptors follow taglib's convention: usually a bare frame ID ("PRIV",
// "GEOB", "POPM"), with a suffix only for UNKNOWN/XXXX, UFID/owner,
// CHAP/elementID, CTOC/elementID. bare IDs remove ALL frames of that type.
// beware: ID3v2 pictures show up here as "APIC" even though they're
// supported via the complex-properties API; removing "APIC" deletes all
// embedded cover art.

__attribute__((export_name("taglib_file_unsupported"))) char **
taglib_file_unsupported(const char *filename) {
  TagLib::FileRef file(filename);
  if (file.isNull())
    return nullptr;

  const auto unsupported = file.properties().unsupportedData();

  size_t len = unsupported.size();
  char **out = static_cast<char **>(malloc(sizeof(char *) * (len + 1)));
  if (!out)
    return nullptr;

  size_t i = 0;
  for (const auto &d : unsupported) {
    out[i] = to_char_array(d);
    i++;
  }
  out[len] = nullptr;

  return out;
}

__attribute__((export_name("taglib_file_remove_unsupported"))) bool
taglib_file_remove_unsupported(const char *filename,
                               const char **descriptors) {
  if (!filename || !descriptors)
    return false;

  TagLib::FileRef file(filename);
  if (file.isNull())
    return false;

  TagLib::StringList list;
  for (size_t i = 0; descriptors[i]; i++)
    list.append(to_string(descriptors[i]));

  file.removeUnsupportedProperties(list);
  return file.save();
}

static const uint8_t CLEAR = 1 << 0;

__attribute__((export_name("taglib_file_write_tags"))) bool
taglib_file_write_tags(const char *filename, const char **tags, uint8_t opts) {
  if (!filename || !tags)
    return false;

  TagLib::FileRef file(filename);
  if (file.isNull())
    return false;

  auto properties = file.properties();
  if (opts & CLEAR)
    properties.clear();

  for (size_t i = 0; tags[i]; i++) {
    TagLib::String row(tags[i], TagLib::String::UTF8);
    if (auto ti = row.find("\t"); ti != -1) {
      auto key = row.substr(0, ti);
      auto value = row.substr(ti + 1);
      if (value.isEmpty())
        properties.erase(key);
      else
        properties.replace(key, value.split("\v"));
    }
  }

  file.setProperties(properties);
  return file.save();
}

struct FileProperties {
  char *format;
  char *innerCodec;
  uint32_t lengthInMilliseconds;
  uint32_t channels;
  uint32_t sampleRate;
  uint32_t bitRate;
  uint32_t bitsPerSample;
  char **imageMetadata;
};

// format, inner codec and bit depth all live on the concrete *::Properties subclass, not the
// base AudioProperties, so downcast once and read them together. names mirror taglib's own,
// lowercased. bitsPerSample() isn't virtual, hence the per-arm calls.
static void extract_format_codec_depth(const TagLib::AudioProperties *ap,
                                       char **format, char **innerCodec, uint32_t *bitsPerSample) {
  using namespace TagLib;
  *format = nullptr;
  *innerCodec = nullptr;
  *bitsPerSample = 0;

  // containers: codec() picks the inner codec
  if (auto p = dynamic_cast<const MP4::Properties *>(ap)) {
    *format = to_char_array("mp4");
    *bitsPerSample = p->bitsPerSample();
    switch (p->codec()) {
      case MP4::Properties::AAC:  *innerCodec = to_char_array("aac");  break;
      case MP4::Properties::ALAC: *innerCodec = to_char_array("alac"); break;
      default: break;
    }
    return;
  }
  if (auto p = dynamic_cast<const ASF::Properties *>(ap)) {
    *format = to_char_array("asf");
    *bitsPerSample = p->bitsPerSample();
    switch (p->codec()) {
      case ASF::Properties::WMA1:         *innerCodec = to_char_array("wma1");         break;
      case ASF::Properties::WMA2:         *innerCodec = to_char_array("wma2");         break;
      case ASF::Properties::WMA9Pro:      *innerCodec = to_char_array("wma9pro");      break;
      case ASF::Properties::WMA9Lossless: *innerCodec = to_char_array("wma9lossless"); break;
      default: break;
    }
    return;
  }

  // ogg has one subclass per codec. ogg flac (.oga) reuses FLAC::Properties so it can't be told
  // apart here and falls through to the flac arm below - fine, we don't ship an .oga fixture
  if (dynamic_cast<const Ogg::Vorbis::Properties *>(ap)) { *format = to_char_array("ogg"); *innerCodec = to_char_array("vorbis"); return; }
  if (dynamic_cast<const Ogg::Opus::Properties *>(ap))   { *format = to_char_array("ogg"); *innerCodec = to_char_array("opus");   return; }
  if (dynamic_cast<const Ogg::Speex::Properties *>(ap))  { *format = to_char_array("ogg"); *innerCodec = to_char_array("speex");  return; }

  // riff: check the format tag rather than assume pcm; compressed wav/aiff-c leaves innerCodec ""
  if (auto p = dynamic_cast<const RIFF::WAV::Properties *>(ap)) {
    *format = to_char_array("wav");
    *bitsPerSample = p->bitsPerSample();
    if (p->format() == 1 || p->format() == 3) *innerCodec = to_char_array("pcm"); // 1 = pcm, 3 = float
    return;
  }
  if (auto p = dynamic_cast<const RIFF::AIFF::Properties *>(ap)) {
    *format = to_char_array("aiff");
    *bitsPerSample = p->bitsPerSample();
    auto c = p->compressionType(); // aiff-c 4cc; these are the uncompressed ones
    if (!p->isAiffC() || c.isEmpty() || c == "NONE" || c == "sowt" || c == "twos" || c == "fl32" || c == "fl64")
      *innerCodec = to_char_array("pcm");
    return;
  }

  // dsf (sony) and dsdiff (philips) are separate formats, both carrying dsd
  if (auto p = dynamic_cast<const DSF::Properties *>(ap))    { *format = to_char_array("dsf");    *innerCodec = to_char_array("dsd"); *bitsPerSample = p->bitsPerSample(); return; }
  if (auto p = dynamic_cast<const DSDIFF::Properties *>(ap)) { *format = to_char_array("dsdiff"); *innerCodec = to_char_array("dsd"); *bitsPerSample = p->bitsPerSample(); return; }

  // monolithic lossless: format is the codec, innerCodec stays ""
  if (auto p = dynamic_cast<const FLAC::Properties *>(ap))      { *format = to_char_array("flac");    *bitsPerSample = p->bitsPerSample(); return; }
  if (auto p = dynamic_cast<const APE::Properties *>(ap))       { *format = to_char_array("ape");     *bitsPerSample = p->bitsPerSample(); return; }
  if (auto p = dynamic_cast<const WavPack::Properties *>(ap))   { *format = to_char_array("wavpack"); *bitsPerSample = p->bitsPerSample(); return; }
  if (auto p = dynamic_cast<const TrueAudio::Properties *>(ap)) { *format = to_char_array("tta");     *bitsPerSample = p->bitsPerSample(); return; }
  if (auto p = dynamic_cast<const Shorten::Properties *>(ap))   { *format = to_char_array("shorten"); *bitsPerSample = p->bitsPerSample(); return; }

  // monolithic lossy: no fixed bit depth
  if (dynamic_cast<const MPEG::Properties *>(ap)) { *format = to_char_array("mpeg");     return; }
  if (dynamic_cast<const MPC::Properties *>(ap))  { *format = to_char_array("musepack"); return; }
  // anything else (tracker modules etc.) stays ""
}

__attribute__((export_name("taglib_file_read_properties"))) FileProperties *
taglib_file_read_properties(const char *filename) {
  TagLib::FileRef file(filename);
  if (file.isNull() || !file.audioProperties())
    return nullptr;

  FileProperties *props =
      static_cast<FileProperties *>(malloc(sizeof(FileProperties)));
  if (!props)
    return nullptr;

  auto audioProperties = file.audioProperties();
  props->lengthInMilliseconds = audioProperties->lengthInMilliseconds();
  props->channels = audioProperties->channels();
  props->sampleRate = audioProperties->sampleRate();
  props->bitRate = audioProperties->bitrate();
  extract_format_codec_depth(audioProperties, &props->format, &props->innerCodec, &props->bitsPerSample);

  const auto &pictures = file.complexProperties("PICTURE");

  props->imageMetadata = nullptr;
  if (pictures.isEmpty())
    return props;

  size_t len = pictures.size();
  char **imageMetadata =
      static_cast<char **>(malloc(sizeof(char *) * (len + 1)));
  if (!imageMetadata)
    return props;

  size_t i = 0;
  for (const auto &p : pictures) {
    TagLib::String type = p["pictureType"].toString();
    TagLib::String desc = p["description"].toString();
    TagLib::String mime = p["mimeType"].toString();
    TagLib::String row = type + "\t" + desc + "\t" + mime;
    imageMetadata[i] = to_char_array(row);
    i++;
  }
  imageMetadata[len] = nullptr;

  props->imageMetadata = imageMetadata;

  return props;
}

struct ByteData {
  uint32_t length;
  char *data;
};

__attribute__((export_name("taglib_file_read_image"))) ByteData *
taglib_file_read_image(const char *filename, int index) {
  TagLib::FileRef file(filename);
  if (file.isNull())
    return nullptr;

  const auto &pictures = file.complexProperties("PICTURE");
  if (pictures.isEmpty())
    return nullptr;

  if (index < 0 || index >= static_cast<int>(pictures.size()))
    return nullptr;

  auto v = pictures[index]["data"].toByteVector();
  ByteData *bd = static_cast<ByteData *>(malloc(sizeof(ByteData)));
  if (!bd)
    return nullptr;

  bd->length = static_cast<uint32_t>(v.size());
  if (bd->length == 0) {
    bd->data = nullptr;
    return bd;
  }

  // allocate and copy into module memory to keep it valid for go to read
  char *buf = static_cast<char *>(malloc(bd->length));
  if (!buf)
    return nullptr;

  memcpy(buf, v.data(), bd->length);
  bd->data = buf;

  return bd;
}

__attribute__((export_name("taglib_file_write_image"))) bool
taglib_file_write_image(const char *filename, const char *buf, uint32_t length,
                        int index, const char *pictureType,
                        const char *description, const char *mimeType) {
  TagLib::FileRef file(filename);
  if (file.isNull())
    return false;

  auto pictures = file.complexProperties("PICTURE");

  if (length == 0) {
    // remove image at index if it exists
    if (index >= 0 && index < static_cast<int>(pictures.size())) {
      auto it = pictures.begin();
      std::advance(it, index);
      pictures.erase(it);
      if (!file.setComplexProperties("PICTURE", pictures))
        return false;
    }
    return file.save();
  }

  TagLib::VariantMap newPicture;
  newPicture["data"] = TagLib::ByteVector(buf, length);
  newPicture["pictureType"] = to_string(pictureType);
  newPicture["description"] = to_string(description);
  newPicture["mimeType"] = to_string(mimeType);

  // replace image at index, or append if index is out of range
  if (index >= 0 && index < static_cast<int>(pictures.size()))
    pictures[index] = newPicture;
  else
    pictures.append(newPicture);

  if (!file.setComplexProperties("PICTURE", pictures))
    return false;

  return file.save();
}
