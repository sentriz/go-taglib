//go:build ignore
#include <cstdint>
#include <cstring>
#include <iostream>
#include <map>

#include "fileref.h"
#include "tiostream.h"
#include "tpropertymap.h"
#include "mpeg/mpegfile.h"
#include "mpeg/id3v1/id3v1tag.h"
#include "mpeg/id3v2/id3v2tag.h"
#include "mpeg/id3v2/frames/textidentificationframe.h"
#include "mpeg/id3v2/frames/commentsframe.h"
#include "mpeg/id3v2/frames/popularimeterframe.h"
#include "mpeg/id3v2/frames/unsynchronizedlyricsframe.h"
#include "mpeg/id3v2/frames/synchronizedlyricsframe.h"
#include "mpeg/mpegproperties.h"
#include "mp4/mp4file.h"
#include "mp4/mp4tag.h"
#include "mp4/mp4item.h"
#include "flac/flacfile.h"
#include "flac/flacproperties.h"
#include "mp4/mp4properties.h"
#include "riff/aiff/aifffile.h"
#include "riff/aiff/aiffproperties.h"
#include "riff/wav/wavfile.h"
#include "riff/wav/wavproperties.h"
#include "ape/apefile.h"
#include "ape/apeproperties.h"
#include "asf/asffile.h"
#include "asf/asfproperties.h"
#include "asf/asftag.h"
#include "asf/asfattribute.h"
#include "wavpack/wavpackfile.h"
#include "wavpack/wavpackproperties.h"
#include "ogg/oggfile.h"
#include "ogg/vorbis/vorbisfile.h"
#include "ogg/flac/oggflacfile.h"
#include "ogg/opus/opusfile.h"
#include "ogg/speex/speexfile.h"
#include "dsf/dsffile.h"
#include "dsf/dsfproperties.h"
#include "dsdiff/dsdifffile.h"
#include "trueaudio/trueaudiofile.h"
#include "mpc/mpcfile.h"
#include "mpc/mpcproperties.h"
#include "shorten/shortenfile.h"
#include "matroska/matroskafile.h"
#include "matroska/matroskatag.h"
#include "matroska/matroskasimpletag.h"

// File format enum - must match Go's FileFormat
enum FileFormat : uint8_t {
  FORMAT_UNKNOWN = 0,
  FORMAT_MPEG = 1,
  FORMAT_MP4 = 2,
  FORMAT_FLAC = 3,
  FORMAT_OGG_VORBIS = 4,
  FORMAT_OGG_OPUS = 5,
  FORMAT_OGG_FLAC = 6,
  FORMAT_OGG_SPEEX = 7,
  FORMAT_WAV = 8,
  FORMAT_AIFF = 9,
  FORMAT_ASF = 10,
  FORMAT_APE = 11,
  FORMAT_WAVPACK = 12,
  FORMAT_DSF = 13,
  FORMAT_DSDIFF = 14,
  FORMAT_TRUE_AUDIO = 15,
  FORMAT_MPC = 16,
  FORMAT_SHORTEN = 17,
  FORMAT_MATROSKA = 18,
};

// ============================================================================
// Host function imports (implemented in Go, called from WASM)
// ============================================================================

extern "C" {
  // Read up to 'length' bytes from stream 'streamId' into buffer at 'bufPtr'.
  // Returns number of bytes actually read.
  __attribute__((import_module("go_io"), import_name("stream_read")))
  uint32_t go_stream_read(uint32_t streamId, uint32_t bufPtr, uint32_t length);

  // Seek to position in stream. whence: 0=Beginning, 1=Current, 2=End
  // Returns 0 on success, non-zero on error.
  __attribute__((import_module("go_io"), import_name("stream_seek")))
  int32_t go_stream_seek(uint32_t streamId, int64_t offset, int32_t whence);

  // Returns current position in stream.
  __attribute__((import_module("go_io"), import_name("stream_tell")))
  int64_t go_stream_tell(uint32_t streamId);

  // Returns total length of stream.
  __attribute__((import_module("go_io"), import_name("stream_length")))
  int64_t go_stream_length(uint32_t streamId);
}

// ============================================================================
// GoIOStream - IOStream implementation backed by Go io.ReadSeeker
// ============================================================================

class GoIOStream : public TagLib::IOStream {
public:
  static constexpr size_t BUFFER_SIZE = 32 * 1024; // 32KB - optimal for all formats

  GoIOStream(uint32_t streamId, const char *filename = "")
    : m_streamId(streamId)
    , m_readOnly(true)
    , m_position(0)
    , m_length(go_stream_length(streamId))
    , m_buffer(nullptr)
    , m_bufStart(-1)
    , m_bufLen(0)
    , m_filename(filename ? filename : "")
  {}

  ~GoIOStream() {
    if (m_buffer) free(m_buffer);
  }

  TagLib::FileName name() const override {
    return m_filename.c_str();
  }

  TagLib::ByteVector readBlock(size_t length) override {
    if (length == 0 || m_position >= m_length) {
      return TagLib::ByteVector();
    }

    size_t available = static_cast<size_t>(m_length - m_position);
    size_t toRead = std::min(length, available);

    TagLib::ByteVector result;
    result.resize(toRead);
    size_t totalRead = 0;

    while (totalRead < toRead) {
      // Check if position is in buffer
      if (m_bufLen > 0 && m_position >= m_bufStart &&
          m_position < m_bufStart + static_cast<int64_t>(m_bufLen)) {
        size_t bufOffset = static_cast<size_t>(m_position - m_bufStart);
        size_t bufAvail = m_bufLen - bufOffset;
        size_t toCopy = std::min(toRead - totalRead, bufAvail);
        memcpy(result.data() + totalRead, m_buffer + bufOffset, toCopy);
        m_position += toCopy;
        totalRead += toCopy;
        continue;
      }

      // Buffer miss - refill
      if (!refillBuffer()) break;
    }

    if (totalRead < toRead) result.resize(totalRead);
    return result;
  }

  void writeBlock(const TagLib::ByteVector &data) override {}
  void insert(const TagLib::ByteVector &data, TagLib::offset_t start, size_t replace) override {}
  void removeBlock(TagLib::offset_t start, size_t length) override {}
  bool readOnly() const override { return m_readOnly; }
  bool isOpen() const override { return true; }

  void seek(TagLib::offset_t offset, Position p = Beginning) override {
    int64_t newPos = 0;
    switch (p) {
      case Beginning: newPos = offset; break;
      case Current:   newPos = m_position + offset; break;
      case End:       newPos = m_length + offset; break;
    }
    if (newPos < 0) newPos = 0;
    if (newPos > m_length) newPos = m_length;
    m_position = newPos;
  }

  void clear() override {}
  TagLib::offset_t tell() const override { return m_position; }
  TagLib::offset_t length() override { return m_length; }
  void truncate(TagLib::offset_t length) override {}

private:
  bool refillBuffer() {
    if (!m_buffer) {
      m_buffer = static_cast<char *>(malloc(BUFFER_SIZE));
      if (!m_buffer) return false;
    }

    go_stream_seek(m_streamId, m_position, 0);

    size_t totalRead = 0;
    while (totalRead < BUFFER_SIZE) {
      uint32_t bytesRead = go_stream_read(m_streamId,
          reinterpret_cast<uint32_t>(m_buffer + totalRead),
          static_cast<uint32_t>(BUFFER_SIZE - totalRead));
      if (bytesRead == 0) break;
      totalRead += bytesRead;
    }

    if (totalRead == 0) return false;
    m_bufStart = m_position;
    m_bufLen = totalRead;
    return true;
  }

  uint32_t m_streamId;
  bool m_readOnly;
  int64_t m_position;
  int64_t m_length;
  char *m_buffer;
  int64_t m_bufStart;
  size_t m_bufLen;
  std::string m_filename;
};

// Handle management
struct FileHandle {
  TagLib::FileRef *fileRef;
  GoIOStream *stream;  // non-null if opened from stream
  FileFormat format;
};

// ============================================================================
// Global handle map and utilities
// ============================================================================
static std::map<uint32_t, FileHandle> g_handles;
static uint32_t g_nextHandle = 1;

static FileFormat detect_format(TagLib::File *file) {
  if (!file) return FORMAT_UNKNOWN;
  if (dynamic_cast<TagLib::MPEG::File *>(file)) return FORMAT_MPEG;
  if (dynamic_cast<TagLib::MP4::File *>(file)) return FORMAT_MP4;
  if (dynamic_cast<TagLib::FLAC::File *>(file)) return FORMAT_FLAC;
  if (dynamic_cast<TagLib::Ogg::Vorbis::File *>(file)) return FORMAT_OGG_VORBIS;
  if (dynamic_cast<TagLib::Ogg::Opus::File *>(file)) return FORMAT_OGG_OPUS;
  if (dynamic_cast<TagLib::Ogg::FLAC::File *>(file)) return FORMAT_OGG_FLAC;
  if (dynamic_cast<TagLib::Ogg::Speex::File *>(file)) return FORMAT_OGG_SPEEX;
  if (dynamic_cast<TagLib::RIFF::WAV::File *>(file)) return FORMAT_WAV;
  if (dynamic_cast<TagLib::RIFF::AIFF::File *>(file)) return FORMAT_AIFF;
  if (dynamic_cast<TagLib::ASF::File *>(file)) return FORMAT_ASF;
  if (dynamic_cast<TagLib::APE::File *>(file)) return FORMAT_APE;
  if (dynamic_cast<TagLib::WavPack::File *>(file)) return FORMAT_WAVPACK;
  if (dynamic_cast<TagLib::DSF::File *>(file)) return FORMAT_DSF;
  if (dynamic_cast<TagLib::DSDIFF::File *>(file)) return FORMAT_DSDIFF;
  if (dynamic_cast<TagLib::TrueAudio::File *>(file)) return FORMAT_TRUE_AUDIO;
  if (dynamic_cast<TagLib::MPC::File *>(file)) return FORMAT_MPC;
  if (dynamic_cast<TagLib::Shorten::File *>(file)) return FORMAT_SHORTEN;
  if (dynamic_cast<TagLib::Matroska::File *>(file)) return FORMAT_MATROSKA;
  return FORMAT_UNKNOWN;
}

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

// ============================================================================
// Handle-based API
// ============================================================================

struct OpenResult {
  uint32_t handle;
  uint8_t format;
};

__attribute__((export_name("taglib_file_open"))) OpenResult *
taglib_file_open(const char *filename, uint8_t readStyle) {
  auto style = static_cast<TagLib::AudioProperties::ReadStyle>(readStyle);
  TagLib::FileRef *fileRef = new TagLib::FileRef(filename, true, style);
  if (fileRef->isNull()) {
    delete fileRef;
    return nullptr;
  }

  OpenResult *result = static_cast<OpenResult *>(malloc(sizeof(OpenResult)));
  if (!result) {
    delete fileRef;
    return nullptr;
  }

  uint32_t handle = g_nextHandle++;
  FileFormat format = detect_format(fileRef->file());

  g_handles[handle] = FileHandle{fileRef, nullptr, format};

  result->handle = handle;
  result->format = static_cast<uint8_t>(format);
  return result;
}

__attribute__((export_name("taglib_file_close"))) void
taglib_file_close(uint32_t handle) {
  auto it = g_handles.find(handle);
  if (it != g_handles.end()) {
    delete it->second.fileRef;
    if (it->second.stream) {
      delete it->second.stream;
    }
    g_handles.erase(it);
  }
}

// Open a file from a Go io.ReadSeeker stream
__attribute__((export_name("taglib_stream_open"))) OpenResult *
taglib_stream_open(uint32_t streamId, const char *filename, uint8_t readStyle) {
  GoIOStream *stream = new GoIOStream(streamId, filename);
  auto style = static_cast<TagLib::AudioProperties::ReadStyle>(readStyle);

  // FileRef takes ownership of the stream pointer for file operations
  // but does NOT delete it - we manage it in FileHandle
  TagLib::FileRef *fileRef = new TagLib::FileRef(stream, true, style);
  if (fileRef->isNull()) {
    delete fileRef;
    delete stream;
    return nullptr;
  }

  OpenResult *result = static_cast<OpenResult *>(malloc(sizeof(OpenResult)));
  if (!result) {
    delete fileRef;
    delete stream;
    return nullptr;
  }

  uint32_t handle = g_nextHandle++;
  FileFormat format = detect_format(fileRef->file());

  g_handles[handle] = FileHandle{fileRef, stream, format};

  result->handle = handle;
  result->format = static_cast<uint8_t>(format);
  return result;
}

// Helper to get FileRef from handle
static TagLib::FileRef *get_file_ref(uint32_t handle) {
  auto it = g_handles.find(handle);
  if (it == g_handles.end()) return nullptr;
  return it->second.fileRef;
}

static FileFormat get_format(uint32_t handle) {
  auto it = g_handles.find(handle);
  if (it == g_handles.end()) return FORMAT_UNKNOWN;
  return it->second.format;
}

// For Matroska files, TagLib's properties() may miss tags written by ffmpeg.
// ffmpeg uses tag names like "ALBUM", "ARTIST" directly as SimpleTag names,
// while the Matroska spec (and TagLib) expects e.g. "TITLE" at Album level
// for the album name. TagLib drops unrecognized names at non-Track levels.
// This helper adds missing properties from the raw SimpleTags and the
// segment title fallback.
static TagLib::PropertyMap enrich_matroska_properties(TagLib::FileRef &fileRef) {
  auto properties = fileRef.properties();
  auto *mkFile = dynamic_cast<TagLib::Matroska::File *>(fileRef.file());
  if (!mkFile)
    return properties;

  auto *mkTag = dynamic_cast<TagLib::Matroska::Tag *>(mkFile->tag());
  if (!mkTag)
    return properties;

  // Matroska SimpleTag names used at Album level by TagLib's translation table.
  // These are the spec-compliant names that TagLib already translates to
  // property keys (e.g. "TITLE" at Album -> "ALBUM"). We skip these to
  // avoid duplicating what properties() already returns.
  static const TagLib::StringList knownAlbumTags = {
    "TITLE", "ARTIST", "PART_NUMBER", "TOTAL_PARTS", "TITLESORT",
    "ARTISTSORT", "REPLAYGAIN_GAIN", "REPLAYGAIN_PEAK",
    "DATE_RELEASED", "LABEL_CODE", "CATALOG_NUMBER",
    "MUSICBRAINZ_ALBUMARTISTID", "MUSICBRAINZ_ALBUMID",
    "MUSICBRAINZ_RELEASEGROUPID",
  };

  // Scan SimpleTags at Album level for ffmpeg-style names that TagLib dropped.
  // ffmpeg writes e.g. "ALBUM" as a SimpleTag name at Album level (50),
  // but TagLib expects "TITLE" at Album level and drops "ALBUM".
  for (const auto &st : mkTag->simpleTagsList()) {
    if (st.type() != TagLib::Matroska::SimpleTag::StringType || st.trackUid() != 0)
      continue;
    if (st.targetTypeValue() != TagLib::Matroska::SimpleTag::Album)
      continue;
    TagLib::String name = st.name();
    if (name.isEmpty() || properties.contains(name))
      continue;
    if (knownAlbumTags.contains(name))
      continue;
    properties[name].append(st.toString());
  }

  // Add segment title as TITLE if still missing
  if (!properties.isEmpty() && !properties.contains("TITLE")) {
    auto *tag = fileRef.tag();
    if (tag && !tag->title().isEmpty())
      properties["TITLE"].append(tag->title());
  }

  return properties;
}

// Helper to serialize properties to string array
static char **serialize_properties(const TagLib::PropertyMap &properties) {
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

__attribute__((export_name("taglib_handle_tags"))) char **
taglib_handle_tags(uint32_t handle) {
  TagLib::FileRef *fileRef = get_file_ref(handle);
  if (!fileRef)
    return nullptr;
  return serialize_properties(enrich_matroska_properties(*fileRef));
}

// Forward declarations for raw tag helpers
static char **read_id3v2_frames_from_tag(TagLib::ID3v2::Tag *id3v2Tag);
static char **read_mp4_items_from_tag(TagLib::MP4::Tag *mp4Tag);
static char **read_asf_attributes_from_tag(TagLib::ASF::Tag *asfTag);

__attribute__((export_name("taglib_handle_raw_tags"))) char **
taglib_handle_raw_tags(uint32_t handle) {
  TagLib::FileRef *fileRef = get_file_ref(handle);
  if (!fileRef || fileRef->isNull())
    return nullptr;

  FileFormat format = get_format(handle);
  TagLib::File *file = fileRef->file();

  // Route based on format
  switch (format) {
    case FORMAT_MPEG: {
      auto *mpegFile = dynamic_cast<TagLib::MPEG::File *>(file);
      if (mpegFile && mpegFile->hasID3v2Tag())
        return read_id3v2_frames_from_tag(mpegFile->ID3v2Tag());
      break;
    }
    case FORMAT_WAV: {
      auto *wavFile = dynamic_cast<TagLib::RIFF::WAV::File *>(file);
      if (wavFile && wavFile->hasID3v2Tag())
        return read_id3v2_frames_from_tag(wavFile->ID3v2Tag());
      break;
    }
    case FORMAT_AIFF: {
      auto *aiffFile = dynamic_cast<TagLib::RIFF::AIFF::File *>(file);
      if (aiffFile && aiffFile->hasID3v2Tag())
        return read_id3v2_frames_from_tag(aiffFile->tag());
      break;
    }
    case FORMAT_MP4: {
      auto *mp4File = dynamic_cast<TagLib::MP4::File *>(file);
      if (mp4File && mp4File->hasMP4Tag())
        return read_mp4_items_from_tag(mp4File->tag());
      break;
    }
    case FORMAT_ASF: {
      auto *asfFile = dynamic_cast<TagLib::ASF::File *>(file);
      if (asfFile && asfFile->tag())
        return read_asf_attributes_from_tag(asfFile->tag());
      break;
    }
    default:
      // For formats without format-specific tags (FLAC, OGG, etc.),
      // return the same as normalized tags (they use Vorbis Comments)
      return serialize_properties(enrich_matroska_properties(*fileRef));
  }

  // Return empty array
  char **empty = static_cast<char **>(malloc(sizeof(char *)));
  if (empty) empty[0] = nullptr;
  return empty;
}

struct FileProperties {
  uint32_t lengthInMilliseconds;
  uint32_t channels;
  uint32_t sampleRate;
  uint32_t bitrate;
  uint32_t bitsPerSample;
  char **imageMetadata;
  char *codec;
};

static int extract_bits_per_sample(const TagLib::AudioProperties *audioProperties) {
  if (const auto* apeProperties = dynamic_cast<const TagLib::APE::Properties*>(audioProperties))
    return apeProperties->bitsPerSample();
  if (const auto* asfProperties = dynamic_cast<const TagLib::ASF::Properties*>(audioProperties))
    return asfProperties->bitsPerSample();
  if (const auto* flacProperties = dynamic_cast<const TagLib::FLAC::Properties*>(audioProperties))
    return flacProperties->bitsPerSample();
  if (const auto* mp4Properties = dynamic_cast<const TagLib::MP4::Properties*>(audioProperties))
    return mp4Properties->bitsPerSample();
  if (const auto* wavPackProperties = dynamic_cast<const TagLib::WavPack::Properties*>(audioProperties))
    return wavPackProperties->bitsPerSample();
  if (const auto* aiffProperties = dynamic_cast<const TagLib::RIFF::AIFF::Properties*>(audioProperties))
    return aiffProperties->bitsPerSample();
  if (const auto* wavProperties = dynamic_cast<const TagLib::RIFF::WAV::Properties*>(audioProperties))
    return wavProperties->bitsPerSample();
  if (const auto* dsfProperties = dynamic_cast<const TagLib::DSF::Properties*>(audioProperties))
    return dsfProperties->bitsPerSample();
  if (const auto* mkProperties = dynamic_cast<const TagLib::Matroska::Properties*>(audioProperties))
    return mkProperties->bitsPerSample();
  return 0;
}

static char* extract_codec(const TagLib::AudioProperties *audioProperties) {
  TagLib::String codec;
  if (const auto* mp4Props = dynamic_cast<const TagLib::MP4::Properties*>(audioProperties)) {
    switch (mp4Props->codec()) {
      case TagLib::MP4::Properties::AAC: codec = "AAC"; break;
      case TagLib::MP4::Properties::ALAC: codec = "ALAC"; break;
      default: break;
    }
  }
  else if (const auto* asfProps = dynamic_cast<const TagLib::ASF::Properties*>(audioProperties)) {
    switch (asfProps->codec()) {
      case TagLib::ASF::Properties::WMA1: codec = "WMA1"; break;
      case TagLib::ASF::Properties::WMA2: codec = "WMA2"; break;
      case TagLib::ASF::Properties::WMA9Pro: codec = "WMA9Pro"; break;
      case TagLib::ASF::Properties::WMA9Lossless: codec = "WMA9Lossless"; break;
      default: break;
    }
  }
  else if (const auto* mpegProps = dynamic_cast<const TagLib::MPEG::Properties*>(audioProperties)) {
    if (mpegProps->isADTS()) {
      codec = "AAC";
    } else {
      switch (mpegProps->layer()) {
        case 1: codec = "MP1"; break;
        case 2: codec = "MP2"; break;
        case 3: codec = "MP3"; break;
        default: break;
      }
    }
  }
  else if (const auto* mpcProps = dynamic_cast<const TagLib::MPC::Properties*>(audioProperties)) {
    int version = mpcProps->mpcVersion();
    if (version >= 8)
      codec = "MPC8";
    else if (version >= 7)
      codec = "MPC7";
  }
  else if (const auto* mkProps = dynamic_cast<const TagLib::Matroska::Properties*>(audioProperties)) {
    codec = mkProps->codecName();
  }
  return codec.isEmpty() ? nullptr : to_char_array(codec);
}

static char** extract_image_metadata(const TagLib::List<TagLib::VariantMap> &pictures) {
  if (pictures.isEmpty())
    return nullptr;

  size_t len = pictures.size();
  char **imageMetadata = static_cast<char **>(malloc(sizeof(char *) * (len + 1)));
  if (!imageMetadata)
    return nullptr;

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
  return imageMetadata;
}

static FileProperties* read_file_properties(TagLib::FileRef &file) {
  if (file.isNull() || !file.audioProperties())
    return nullptr;

  FileProperties *props = static_cast<FileProperties *>(malloc(sizeof(FileProperties)));
  if (!props)
    return nullptr;

  auto audioProperties = file.audioProperties();
  props->lengthInMilliseconds = audioProperties->lengthInMilliseconds();
  props->channels = audioProperties->channels();
  props->sampleRate = audioProperties->sampleRate();
  props->bitrate = audioProperties->bitrate();
  props->bitsPerSample = extract_bits_per_sample(audioProperties);
  props->codec = extract_codec(audioProperties);
  props->imageMetadata = extract_image_metadata(file.complexProperties("PICTURE"));

  return props;
}

__attribute__((export_name("taglib_handle_properties"))) FileProperties *
taglib_handle_properties(uint32_t handle) {
  TagLib::FileRef *fileRef = get_file_ref(handle);
  if (!fileRef)
    return nullptr;
  return read_file_properties(*fileRef);
}

struct ByteData {
  uint32_t length;
  char *data;
};

static ByteData* read_image(TagLib::FileRef &file, int index) {
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

  char *buf = static_cast<char *>(malloc(bd->length));
  if (!buf)
    return nullptr;

  memcpy(buf, v.data(), bd->length);
  bd->data = buf;

  return bd;
}

__attribute__((export_name("taglib_handle_image"))) ByteData *
taglib_handle_image(uint32_t handle, int index) {
  TagLib::FileRef *fileRef = get_file_ref(handle);
  if (!fileRef)
    return nullptr;
  return read_image(*fileRef, index);
}

static const uint8_t CLEAR = 1 << 0;

static bool write_tags(TagLib::FileRef &file, const char **tags, uint8_t opts) {
  if (file.isNull() || !tags)
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

__attribute__((export_name("taglib_handle_write_tags"))) bool
taglib_handle_write_tags(uint32_t handle, const char **tags, uint8_t opts) {
  TagLib::FileRef *fileRef = get_file_ref(handle);
  if (!fileRef)
    return false;
  return write_tags(*fileRef, tags, opts);
}

static bool write_image(TagLib::FileRef &file, const char *buf, uint32_t length,
                       int index, const char *pictureType,
                       const char *description, const char *mimeType) {
  if (file.isNull())
    return false;

  auto pictures = file.complexProperties("PICTURE");

  if (length == 0) {
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

  if (index >= 0 && index < static_cast<int>(pictures.size()))
    pictures[index] = newPicture;
  else
    pictures.append(newPicture);

  if (!file.setComplexProperties("PICTURE", pictures))
    return false;

  return file.save();
}

__attribute__((export_name("taglib_handle_write_image"))) bool
taglib_handle_write_image(uint32_t handle, const char *buf, uint32_t length,
                          int index, const char *pictureType,
                          const char *description, const char *mimeType) {
  TagLib::FileRef *fileRef = get_file_ref(handle);
  if (!fileRef)
    return false;
  return write_image(*fileRef, buf, length, index, pictureType, description, mimeType);
}

// ============================================================================
// Helper functions for raw tag extraction (shared by handle and path APIs)
// ============================================================================

static char **read_id3v2_frames_from_tag(TagLib::ID3v2::Tag *id3v2Tag) {
  if (!id3v2Tag) {
    char **empty = static_cast<char **>(malloc(sizeof(char *)));
    if (empty) empty[0] = nullptr;
    return empty;
  }

  const TagLib::ID3v2::FrameListMap &frameListMap = id3v2Tag->frameListMap();

  size_t frameCount = 0;
  for (TagLib::ID3v2::FrameListMap::ConstIterator it = frameListMap.begin(); it != frameListMap.end(); ++it) {
    frameCount += it->second.size();
  }

  if (frameCount == 0) {
    char **empty = static_cast<char **>(malloc(sizeof(char *)));
    if (empty) empty[0] = nullptr;
    return empty;
  }

  char **frames = static_cast<char **>(malloc(sizeof(char *) * (frameCount + 1)));
  if (!frames) return nullptr;

  size_t i = 0;
  for (TagLib::ID3v2::FrameListMap::ConstIterator it = frameListMap.begin(); it != frameListMap.end(); ++it) {
    TagLib::String frameID = TagLib::String(it->first);

    for (TagLib::ID3v2::FrameList::ConstIterator frameIt = it->second.begin(); frameIt != it->second.end(); ++frameIt) {
      TagLib::String key = frameID;
      TagLib::String value;

      if (frameID == "TXXX") {
        auto userFrame = dynamic_cast<TagLib::ID3v2::UserTextIdentificationFrame *>(*frameIt);
        if (userFrame) {
          key = frameID + ":" + userFrame->description();
          if (!userFrame->fieldList().isEmpty()) {
            value = userFrame->fieldList().back();
          }
        }
      }
      else if (frameID == "COMM") {
        auto commFrame = dynamic_cast<TagLib::ID3v2::CommentsFrame *>(*frameIt);
        if (commFrame) {
          key = frameID + ":" + commFrame->description();
          value = commFrame->text();
        }
      }
      else if (frameID == "POPM") {
        auto popmFrame = dynamic_cast<TagLib::ID3v2::PopularimeterFrame *>(*frameIt);
        if (popmFrame) {
          key = frameID + ":" + popmFrame->email();
          value = TagLib::String::number(popmFrame->rating());
        }
      }
      else if (frameID == "USLT") {
        auto usltFrame = dynamic_cast<TagLib::ID3v2::UnsynchronizedLyricsFrame *>(*frameIt);
        if (usltFrame) {
          TagLib::ByteVector lang = usltFrame->language();
          TagLib::String langStr = "xxx";
          if (lang.size() == 3) {
            char langBuf[4] = {0};
            memcpy(langBuf, lang.data(), 3);
            langStr = TagLib::String(langBuf);
          }
          key = frameID + ":" + langStr;
          value = usltFrame->text();
        }
      }
      else if (frameID == "SYLT") {
        auto syltFrame = dynamic_cast<TagLib::ID3v2::SynchronizedLyricsFrame *>(*frameIt);
        if (syltFrame) {
          TagLib::ByteVector lang = syltFrame->language();
          TagLib::String langStr = "xxx";
          if (lang.size() == 3) {
            char langBuf[4] = {0};
            memcpy(langBuf, lang.data(), 3);
            langStr = TagLib::String(langBuf);
          }
          key = frameID + ":" + langStr;

          TagLib::String lrc;
          auto format = syltFrame->timestampFormat();
          for (const auto &syncText : syltFrame->synchedText()) {
            int timeMs = syncText.time;
            if (format == TagLib::ID3v2::SynchronizedLyricsFrame::AbsoluteMpegFrames)
              continue;
            int mins = timeMs / 60000;
            int secs = (timeMs % 60000) / 1000;
            int centis = (timeMs % 1000) / 10;
            char timeBuf[16];
            snprintf(timeBuf, sizeof(timeBuf), "[%02d:%02d.%02d]", mins, secs, centis);
            lrc = lrc + TagLib::String(timeBuf) + syncText.text + "\n";
          }
          value = lrc;
        }
      }
      else {
        value = (*frameIt)->toString();
      }

      TagLib::String row = key + "\t" + value;
      frames[i++] = to_char_array(row);
    }
  }

  frames[i] = nullptr;
  return frames;
}

static char **read_mp4_items_from_tag(TagLib::MP4::Tag *mp4Tag) {
  if (!mp4Tag) {
    char **empty = static_cast<char **>(malloc(sizeof(char *)));
    if (empty) empty[0] = nullptr;
    return empty;
  }

  const TagLib::MP4::ItemMap &itemMap = mp4Tag->itemMap();

  size_t atomCount = 0;
  for (auto it = itemMap.begin(); it != itemMap.end(); ++it) {
    TagLib::MP4::Item item = it->second;
    switch (item.type()) {
      case TagLib::MP4::Item::Type::StringList:
        atomCount += item.toStringList().size();
        break;
      case TagLib::MP4::Item::Type::IntPair:
        atomCount += 2;
        break;
      default:
        atomCount++;
        break;
    }
  }

  if (atomCount == 0) {
    char **empty = static_cast<char **>(malloc(sizeof(char *)));
    if (empty) empty[0] = nullptr;
    return empty;
  }

  char **atoms = static_cast<char **>(malloc(sizeof(char *) * (atomCount + 1)));
  if (!atoms) return nullptr;

  size_t i = 0;
  for (auto it = itemMap.begin(); it != itemMap.end(); ++it) {
    TagLib::String key = it->first;
    TagLib::MP4::Item item = it->second;

    switch (item.type()) {
      case TagLib::MP4::Item::Type::Bool: {
        TagLib::String value = item.toBool() ? "1" : "0";
        atoms[i++] = to_char_array(key + "\t" + value);
        break;
      }
      case TagLib::MP4::Item::Type::Int: {
        atoms[i++] = to_char_array(key + "\t" + TagLib::String::number(item.toInt()));
        break;
      }
      case TagLib::MP4::Item::Type::IntPair: {
        auto pair = item.toIntPair();
        atoms[i++] = to_char_array(key + ":num\t" + TagLib::String::number(pair.first));
        atoms[i++] = to_char_array(key + ":total\t" + TagLib::String::number(pair.second));
        break;
      }
      case TagLib::MP4::Item::Type::Byte: {
        atoms[i++] = to_char_array(key + "\t" + TagLib::String::number(item.toByte()));
        break;
      }
      case TagLib::MP4::Item::Type::UInt: {
        atoms[i++] = to_char_array(key + "\t" + TagLib::String::number(item.toUInt()));
        break;
      }
      case TagLib::MP4::Item::Type::LongLong: {
        atoms[i++] = to_char_array(key + "\t" + TagLib::String::number(item.toLongLong()));
        break;
      }
      case TagLib::MP4::Item::Type::StringList: {
        for (const auto &s : item.toStringList()) {
          atoms[i++] = to_char_array(key + "\t" + s);
        }
        break;
      }
      case TagLib::MP4::Item::Type::CoverArtList:
      case TagLib::MP4::Item::Type::ByteVectorList: {
        atoms[i++] = to_char_array(key + "\t");
        break;
      }
      default:
        break;
    }
  }

  atoms[i] = nullptr;
  return atoms;
}

static char **read_asf_attributes_from_tag(TagLib::ASF::Tag *asfTag) {
  if (!asfTag) {
    char **empty = static_cast<char **>(malloc(sizeof(char *)));
    if (empty) empty[0] = nullptr;
    return empty;
  }

  const TagLib::ASF::AttributeListMap &attrMap = asfTag->attributeListMap();

  size_t basicCount = 0;
  if (!asfTag->title().isEmpty()) basicCount++;
  if (!asfTag->artist().isEmpty()) basicCount++;
  if (!asfTag->copyright().isEmpty()) basicCount++;
  if (!asfTag->comment().isEmpty()) basicCount++;
  if (!asfTag->rating().isEmpty()) basicCount++;

  size_t attrCount = basicCount;
  for (auto it = attrMap.begin(); it != attrMap.end(); ++it) {
    attrCount += it->second.size();
  }

  if (attrCount == 0) {
    char **empty = static_cast<char **>(malloc(sizeof(char *)));
    if (empty) empty[0] = nullptr;
    return empty;
  }

  char **attrs = static_cast<char **>(malloc(sizeof(char *) * (attrCount + 1)));
  if (!attrs) return nullptr;

  size_t i = 0;

  if (!asfTag->title().isEmpty())
    attrs[i++] = to_char_array(TagLib::String("Title\t") + asfTag->title());
  if (!asfTag->artist().isEmpty())
    attrs[i++] = to_char_array(TagLib::String("Author\t") + asfTag->artist());
  if (!asfTag->copyright().isEmpty())
    attrs[i++] = to_char_array(TagLib::String("Copyright\t") + asfTag->copyright());
  if (!asfTag->comment().isEmpty())
    attrs[i++] = to_char_array(TagLib::String("Description\t") + asfTag->comment());
  if (!asfTag->rating().isEmpty())
    attrs[i++] = to_char_array(TagLib::String("Rating\t") + asfTag->rating());

  for (auto it = attrMap.begin(); it != attrMap.end(); ++it) {
    TagLib::String key = it->first;
    for (const auto &attr : it->second) {
      TagLib::String value;
      switch (attr.type()) {
        case TagLib::ASF::Attribute::UnicodeType:
          value = attr.toString();
          break;
        case TagLib::ASF::Attribute::BoolType:
          value = attr.toBool() ? "1" : "0";
          break;
        case TagLib::ASF::Attribute::DWordType:
          value = TagLib::String::number(attr.toUInt());
          break;
        case TagLib::ASF::Attribute::QWordType:
          value = TagLib::String::number(static_cast<long long>(attr.toULongLong()));
          break;
        case TagLib::ASF::Attribute::WordType:
          value = TagLib::String::number(attr.toUShort());
          break;
        case TagLib::ASF::Attribute::BytesType:
        case TagLib::ASF::Attribute::GuidType:
          value = "";
          break;
        default:
          continue;
      }
      attrs[i++] = to_char_array(key + "\t" + value);
    }
  }

  attrs[i] = nullptr;
  return attrs;
}

// ============================================================================
// Path-based API (legacy)
// ============================================================================

__attribute__((export_name("taglib_file_tags"))) char **
taglib_file_tags(const char *filename) {
  TagLib::FileRef file(filename);
  if (file.isNull())
    return nullptr;
  return serialize_properties(enrich_matroska_properties(file));
}

__attribute__((export_name("taglib_file_write_tags"))) bool
taglib_file_write_tags(const char *filename, const char **tags, uint8_t opts) {
  if (!filename)
    return false;
  TagLib::FileRef file(filename);
  return write_tags(file, tags, opts);
}

__attribute__((export_name("taglib_file_read_properties"))) FileProperties *
taglib_file_read_properties(const char *filename) {
  TagLib::FileRef file(filename);
  return read_file_properties(file);
}

__attribute__((export_name("taglib_file_read_image"))) ByteData *
taglib_file_read_image(const char *filename, int index) {
  TagLib::FileRef file(filename);
  return read_image(file, index);
}

__attribute__((export_name("taglib_file_write_image"))) bool
taglib_file_write_image(const char *filename, const char *buf, uint32_t length,
                        int index, const char *pictureType,
                        const char *description, const char *mimeType) {
  TagLib::FileRef file(filename);
  return write_image(file, buf, length, index, pictureType, description, mimeType);
}

__attribute__((export_name("taglib_file_id3v2_frames"))) char **
taglib_file_id3v2_frames(const char *filename) {
  // Check if file has ID3v2 tags (supports MP3, WAV, AIFF)
  TagLib::FileRef fileRef(filename);
  if (fileRef.isNull())
    return nullptr;

  TagLib::ID3v2::Tag *id3v2Tag = nullptr;

  // Try MP3 first
  if (TagLib::MPEG::File *mpegFile = dynamic_cast<TagLib::MPEG::File *>(fileRef.file())) {
    if (mpegFile->hasID3v2Tag()) {
      id3v2Tag = mpegFile->ID3v2Tag();
    }
  }
  // Try WAV
  else if (TagLib::RIFF::WAV::File *wavFile = dynamic_cast<TagLib::RIFF::WAV::File *>(fileRef.file())) {
    if (wavFile->hasID3v2Tag()) {
      id3v2Tag = wavFile->ID3v2Tag();
    }
  }
  // Try AIFF
  else if (TagLib::RIFF::AIFF::File *aiffFile = dynamic_cast<TagLib::RIFF::AIFF::File *>(fileRef.file())) {
    if (aiffFile->hasID3v2Tag()) {
      id3v2Tag = aiffFile->tag();
    }
  }

  if (!id3v2Tag) {
    // Return empty array instead of nullptr when there are no ID3v2 tags
    char **emptyFrames = static_cast<char **>(malloc(sizeof(char *)));
    if (!emptyFrames)
      return nullptr;
    emptyFrames[0] = nullptr;
    return emptyFrames;
  }
  const TagLib::ID3v2::FrameListMap &frameListMap = id3v2Tag->frameListMap();

  // Count total number of frames
  size_t frameCount = 0;
  for (TagLib::ID3v2::FrameListMap::ConstIterator it = frameListMap.begin(); it != frameListMap.end(); ++it) {
    frameCount += it->second.size();
  }

  if (frameCount == 0) {
    // Return empty array if there are no frames
    char **emptyFrames = static_cast<char **>(malloc(sizeof(char *)));
    if (!emptyFrames)
      return nullptr;
    emptyFrames[0] = nullptr;
    return emptyFrames;
  }

  // Allocate result array
  char **frames = static_cast<char **>(malloc(sizeof(char *) * (frameCount + 1)));
  if (!frames)
    return nullptr;

  size_t i = 0;

  // Process each frame
  for (TagLib::ID3v2::FrameListMap::ConstIterator it = frameListMap.begin(); it != frameListMap.end(); ++it) {
    TagLib::String frameID = TagLib::String(it->first);

    for (TagLib::ID3v2::FrameList::ConstIterator frameIt = it->second.begin(); frameIt != it->second.end(); ++frameIt) {
      TagLib::String key = frameID;
      TagLib::String value;

      // Handle special frame types
      if (frameID == "TXXX") {
        // User text identification frame
        auto userFrame = dynamic_cast<TagLib::ID3v2::UserTextIdentificationFrame *>(*frameIt);
        if (userFrame) {
          key = frameID + ":" + userFrame->description();
          if (!userFrame->fieldList().isEmpty()) {
            value = userFrame->fieldList().back();
          }
        }
      }
      else if (frameID == "COMM") {
        // Comments frame
        auto commFrame = dynamic_cast<TagLib::ID3v2::CommentsFrame *>(*frameIt);
        if (commFrame) {
          key = frameID + ":" + commFrame->description();
          value = commFrame->text();
        }
      }
      else if (frameID == "POPM") {
        // Popularimeter frame (used for WMP ratings)
        auto popmFrame = dynamic_cast<TagLib::ID3v2::PopularimeterFrame *>(*frameIt);
        if (popmFrame) {
          key = frameID + ":" + popmFrame->email();
          value = TagLib::String::number(popmFrame->rating());
        }
      }
      else if (frameID == "USLT") {
        // Unsynchronized lyrics frame
        auto usltFrame = dynamic_cast<TagLib::ID3v2::UnsynchronizedLyricsFrame *>(*frameIt);
        if (usltFrame) {
          // Get language code (3 characters, e.g., "eng", "xxx")
          TagLib::ByteVector lang = usltFrame->language();
          TagLib::String langStr = "xxx";
          if (lang.size() == 3) {
            char langBuf[4] = {0};
            memcpy(langBuf, lang.data(), 3);
            langStr = TagLib::String(langBuf);
          }
          key = frameID + ":" + langStr;
          value = usltFrame->text();
        }
      }
      else if (frameID == "SYLT") {
        // Synchronized lyrics frame - convert to LRC format
        auto syltFrame = dynamic_cast<TagLib::ID3v2::SynchronizedLyricsFrame *>(*frameIt);
        if (syltFrame) {
          // Get language code (3 characters)
          TagLib::ByteVector lang = syltFrame->language();
          TagLib::String langStr = "xxx";
          if (lang.size() == 3) {
            char langBuf[4] = {0};
            memcpy(langBuf, lang.data(), 3);
            langStr = TagLib::String(langBuf);
          }
          key = frameID + ":" + langStr;

          // Build LRC format from synchronized text
          TagLib::String lrc;
          auto format = syltFrame->timestampFormat();
          for (const auto &syncText : syltFrame->synchedText()) {
            int timeMs = syncText.time;
            if (format == TagLib::ID3v2::SynchronizedLyricsFrame::AbsoluteMpegFrames) {
              // Skip MPEG frames format - would need sample rate to convert
              continue;
            }
            int mins = timeMs / 60000;
            int secs = (timeMs % 60000) / 1000;
            int centis = (timeMs % 1000) / 10;
            char timeBuf[16];
            snprintf(timeBuf, sizeof(timeBuf), "[%02d:%02d.%02d]", mins, secs, centis);
            lrc = lrc + TagLib::String(timeBuf) + syncText.text + "\n";
          }
          value = lrc;
        }
      }
      else {
        // Standard frame
        value = (*frameIt)->toString();
      }

      // Create the output string
      TagLib::String row = key + "\t" + value;
      frames[i++] = to_char_array(row);
    }
  }

  frames[i] = nullptr;
  return frames;
}

__attribute__((export_name("taglib_file_id3v1_tags"))) char **
taglib_file_id3v1_tags(const char *filename) {
  // First check if this is an MP3 file with ID3v1 tags
  TagLib::FileRef fileRef(filename);
  if (fileRef.isNull())
    return nullptr;

  // Try to cast to MPEG::File
  TagLib::MPEG::File *mpegFile = dynamic_cast<TagLib::MPEG::File *>(fileRef.file());
  if (!mpegFile || !mpegFile->hasID3v1Tag()) {
    // Return empty array instead of nullptr when there are no ID3v1 tags
    char **emptyTags = static_cast<char **>(malloc(sizeof(char *)));
    if (!emptyTags)
      return nullptr;
    emptyTags[0] = nullptr;
    return emptyTags;
  }

  TagLib::ID3v1::Tag *id3v1Tag = mpegFile->ID3v1Tag();

  // ID3v1 has a fixed set of fields
  const int fieldCount = 7; // title, artist, album, year, comment, track, genre
  char **tags = static_cast<char **>(malloc(sizeof(char *) * (fieldCount + 1)));
  if (!tags)
    return nullptr;

  int i = 0;

  // Add each standard ID3v1 field
  if (!id3v1Tag->title().isEmpty())
    tags[i++] = to_char_array(TagLib::String("TITLE\t") + id3v1Tag->title());

  if (!id3v1Tag->artist().isEmpty())
    tags[i++] = to_char_array(TagLib::String("ARTIST\t") + id3v1Tag->artist());

  if (!id3v1Tag->album().isEmpty())
    tags[i++] = to_char_array(TagLib::String("ALBUM\t") + id3v1Tag->album());

  // Year is an unsigned int in ID3v1, convert to string
  if (id3v1Tag->year() > 0)
    tags[i++] = to_char_array(TagLib::String("YEAR\t") + TagLib::String::number(id3v1Tag->year()));

  if (!id3v1Tag->comment().isEmpty())
    tags[i++] = to_char_array(TagLib::String("COMMENT\t") + id3v1Tag->comment());

  if (id3v1Tag->track() > 0)
    tags[i++] = to_char_array(TagLib::String("TRACK\t") + TagLib::String::number(id3v1Tag->track()));

  // Genre is an int in ID3v1, need to get the string representation
  if (id3v1Tag->genreNumber() != 255) { // 255 is used for "unknown genre"
    if (!id3v1Tag->genre().isEmpty())
      tags[i++] = to_char_array(TagLib::String("GENRE\t") + id3v1Tag->genre());
  }

  tags[i] = nullptr;
  return tags;
}

__attribute__((export_name("taglib_file_mp4_atoms"))) char **
taglib_file_mp4_atoms(const char *filename) {
  TagLib::FileRef fileRef(filename);
  if (fileRef.isNull())
    return nullptr;

  // Try to cast to MP4::File
  TagLib::MP4::File *mp4File = dynamic_cast<TagLib::MP4::File *>(fileRef.file());
  if (!mp4File || !mp4File->hasMP4Tag()) {
    // Return empty array instead of nullptr when there are no MP4 atoms
    char **emptyAtoms = static_cast<char **>(malloc(sizeof(char *)));
    if (!emptyAtoms)
      return nullptr;
    emptyAtoms[0] = nullptr;
    return emptyAtoms;
  }

  TagLib::MP4::Tag *mp4Tag = mp4File->tag();
  const TagLib::MP4::ItemMap &itemMap = mp4Tag->itemMap();

  // First pass: count total entries (multi-value items count as multiple)
  size_t atomCount = 0;
  for (auto it = itemMap.begin(); it != itemMap.end(); ++it) {
    TagLib::MP4::Item item = it->second;
    switch (item.type()) {
      case TagLib::MP4::Item::Type::StringList:
        atomCount += item.toStringList().size();
        break;
      case TagLib::MP4::Item::Type::IntPair:
        atomCount += 2; // num and total as separate keys
        break;
      default:
        atomCount++;
        break;
    }
  }

  if (atomCount == 0) {
    char **emptyAtoms = static_cast<char **>(malloc(sizeof(char *)));
    if (!emptyAtoms)
      return nullptr;
    emptyAtoms[0] = nullptr;
    return emptyAtoms;
  }

  char **atoms = static_cast<char **>(malloc(sizeof(char *) * (atomCount + 1)));
  if (!atoms)
    return nullptr;

  size_t i = 0;
  for (auto it = itemMap.begin(); it != itemMap.end(); ++it) {
    TagLib::String key = it->first;
    TagLib::MP4::Item item = it->second;

    switch (item.type()) {
      case TagLib::MP4::Item::Type::Bool: {
        TagLib::String value = item.toBool() ? "1" : "0";
        TagLib::String row = key + "\t" + value;
        atoms[i++] = to_char_array(row);
        break;
      }
      case TagLib::MP4::Item::Type::Int: {
        TagLib::String value = TagLib::String::number(item.toInt());
        TagLib::String row = key + "\t" + value;
        atoms[i++] = to_char_array(row);
        break;
      }
      case TagLib::MP4::Item::Type::IntPair: {
        auto pair = item.toIntPair();
        TagLib::String numRow = key + ":num\t" + TagLib::String::number(pair.first);
        TagLib::String totalRow = key + ":total\t" + TagLib::String::number(pair.second);
        atoms[i++] = to_char_array(numRow);
        atoms[i++] = to_char_array(totalRow);
        break;
      }
      case TagLib::MP4::Item::Type::Byte: {
        TagLib::String value = TagLib::String::number(item.toByte());
        TagLib::String row = key + "\t" + value;
        atoms[i++] = to_char_array(row);
        break;
      }
      case TagLib::MP4::Item::Type::UInt: {
        TagLib::String value = TagLib::String::number(item.toUInt());
        TagLib::String row = key + "\t" + value;
        atoms[i++] = to_char_array(row);
        break;
      }
      case TagLib::MP4::Item::Type::LongLong: {
        TagLib::String value = TagLib::String::number(item.toLongLong());
        TagLib::String row = key + "\t" + value;
        atoms[i++] = to_char_array(row);
        break;
      }
      case TagLib::MP4::Item::Type::StringList: {
        TagLib::StringList sl = item.toStringList();
        for (const auto &s : sl) {
          TagLib::String row = key + "\t" + s;
          atoms[i++] = to_char_array(row);
        }
        break;
      }
      case TagLib::MP4::Item::Type::CoverArtList:
      case TagLib::MP4::Item::Type::ByteVectorList: {
        // Include binary data atoms with empty value (like ID3v2 does for APIC)
        TagLib::String row = key + "\t";
        atoms[i++] = to_char_array(row);
        break;
      }
      default:
        break;
    }
  }

  atoms[i] = nullptr;
  return atoms;
}

__attribute__((export_name("taglib_file_asf_attributes"))) char **
taglib_file_asf_attributes(const char *filename) {
  TagLib::FileRef fileRef(filename);
  if (fileRef.isNull())
    return nullptr;

  // Try to cast to ASF::File
  TagLib::ASF::File *asfFile = dynamic_cast<TagLib::ASF::File *>(fileRef.file());
  if (!asfFile || !asfFile->tag()) {
    // Return empty array instead of nullptr when there are no ASF attributes
    char **emptyAttrs = static_cast<char **>(malloc(sizeof(char *)));
    if (!emptyAttrs)
      return nullptr;
    emptyAttrs[0] = nullptr;
    return emptyAttrs;
  }

  TagLib::ASF::Tag *asfTag = asfFile->tag();
  const TagLib::ASF::AttributeListMap &attrMap = asfTag->attributeListMap();

  // Count basic fields (Title, Author, Copyright, Description, Rating)
  size_t basicCount = 0;
  if (!asfTag->title().isEmpty()) basicCount++;
  if (!asfTag->artist().isEmpty()) basicCount++;
  if (!asfTag->copyright().isEmpty()) basicCount++;
  if (!asfTag->comment().isEmpty()) basicCount++;
  if (!asfTag->rating().isEmpty()) basicCount++;

  // Count total entries (multi-value attributes count as multiple)
  size_t attrCount = basicCount;
  for (auto it = attrMap.begin(); it != attrMap.end(); ++it) {
    attrCount += it->second.size();
  }

  if (attrCount == 0) {
    char **emptyAttrs = static_cast<char **>(malloc(sizeof(char *)));
    if (!emptyAttrs)
      return nullptr;
    emptyAttrs[0] = nullptr;
    return emptyAttrs;
  }

  char **attrs = static_cast<char **>(malloc(sizeof(char *) * (attrCount + 1)));
  if (!attrs)
    return nullptr;

  size_t i = 0;

  // Add basic fields first (these are stored separately from the attributeListMap)
  if (!asfTag->title().isEmpty()) {
    TagLib::String row = TagLib::String("Title\t") + asfTag->title();
    attrs[i++] = to_char_array(row);
  }
  if (!asfTag->artist().isEmpty()) {
    TagLib::String row = TagLib::String("Author\t") + asfTag->artist();
    attrs[i++] = to_char_array(row);
  }
  if (!asfTag->copyright().isEmpty()) {
    TagLib::String row = TagLib::String("Copyright\t") + asfTag->copyright();
    attrs[i++] = to_char_array(row);
  }
  if (!asfTag->comment().isEmpty()) {
    TagLib::String row = TagLib::String("Description\t") + asfTag->comment();
    attrs[i++] = to_char_array(row);
  }
  if (!asfTag->rating().isEmpty()) {
    TagLib::String row = TagLib::String("Rating\t") + asfTag->rating();
    attrs[i++] = to_char_array(row);
  }

  // Add extended attributes
  for (auto it = attrMap.begin(); it != attrMap.end(); ++it) {
    TagLib::String key = it->first;
    const TagLib::ASF::AttributeList &attrList = it->second;

    for (const auto &attr : attrList) {
      TagLib::String value;
      switch (attr.type()) {
        case TagLib::ASF::Attribute::UnicodeType:
          value = attr.toString();
          break;
        case TagLib::ASF::Attribute::BoolType:
          value = attr.toBool() ? "1" : "0";
          break;
        case TagLib::ASF::Attribute::DWordType:
          value = TagLib::String::number(attr.toUInt());
          break;
        case TagLib::ASF::Attribute::QWordType:
          value = TagLib::String::number(static_cast<long long>(attr.toULongLong()));
          break;
        case TagLib::ASF::Attribute::WordType:
          value = TagLib::String::number(attr.toUShort());
          break;
        case TagLib::ASF::Attribute::BytesType:
        case TagLib::ASF::Attribute::GuidType:
          // Binary data - include with empty value (like ID3v2 does for APIC)
          value = "";
          break;
        default:
          continue;
      }

      TagLib::String row = key + "\t" + value;
      attrs[i++] = to_char_array(row);
    }
  }

  attrs[i] = nullptr;
  return attrs;
}

__attribute__((export_name("taglib_file_write_id3v2_frames"))) bool
taglib_file_write_id3v2_frames(const char *filename, const char **frames, uint8_t opts) {
  if (!filename || !frames)
    return false;

  // First check if this is an MP3 file with ID3v2 tags
  TagLib::MPEG::File file(filename);
  if (!file.isValid())
    return false;

  // Create a new ID3v2 tag if one doesn't exist
  if (!file.hasID3v2Tag()) {
    file.ID3v2Tag(true);
  }

  TagLib::ID3v2::Tag *id3v2Tag = file.ID3v2Tag();

  // If clear option is set, collect all frame IDs we want to keep
  bool clearFrames = (opts & CLEAR);

  // First collect all the frame IDs we're going to set
  std::vector<TagLib::ByteVector> frameIDsToKeep;
  if (clearFrames) {
    for (int i = 0; frames[i] != nullptr; i++) {
      TagLib::String row(frames[i], TagLib::String::UTF8);
      int ti = row.find("\t");
      if (ti != -1) {
        TagLib::String key = row.substr(0, ti);
        // Store the base frame ID (without description for TXXX, COMM, etc.)
        if (key.find(":") != -1) {
          key = key.substr(0, key.find(":"));
        }
        frameIDsToKeep.push_back(key.data(TagLib::String::Latin1));
      }
    }

    // Now remove all frames except those we're going to set
    const TagLib::ID3v2::FrameListMap &frameListMap = id3v2Tag->frameListMap();
    for (TagLib::ID3v2::FrameListMap::ConstIterator it = frameListMap.begin();
         it != frameListMap.end(); ++it) {
      bool keepFrame = false;
      for (size_t i = 0; i < frameIDsToKeep.size(); ++i) {
        if (it->first == frameIDsToKeep[i]) {
          keepFrame = true;
          break;
        }
      }
      if (!keepFrame) {
        id3v2Tag->removeFrames(it->first);
      }
    }
  }

  // Now add the new frames
  for (int i = 0; frames[i] != nullptr; i++) {
    TagLib::String row(frames[i], TagLib::String::UTF8);
    int ti = row.find("\t");
    if (ti != -1) {
      TagLib::String key = row.substr(0, ti);
      TagLib::String value = row.substr(ti + 1);

      // Remove existing frames with this ID
      id3v2Tag->removeFrames(key.toCString(true));

      // Add new frame if value is not empty
      if (!value.isEmpty()) {
        if (key.startsWith("T")) {
          // Text identification frame
          auto newFrame = new TagLib::ID3v2::TextIdentificationFrame(key.toCString(true), TagLib::String::UTF8);
          TagLib::StringList values;

          // Split value by vertical tab
          int pos = 0;
          while (pos != -1) {
            int nextPos = value.find("\v", pos);
            if (nextPos == -1) {
              values.append(value.substr(pos));
              break;
            } else {
              values.append(value.substr(pos, nextPos - pos));
              pos = nextPos + 1;
            }
          }

          newFrame->setText(values);
          id3v2Tag->addFrame(newFrame);
        }
        else if (key == "COMM") {
          // Comments frame
          auto newFrame = new TagLib::ID3v2::CommentsFrame(TagLib::String::UTF8);
          newFrame->setText(value);
          id3v2Tag->addFrame(newFrame);
        }
        // Add other frame types as needed
      }
    }
  }

  // Save the file
  return file.save();
}