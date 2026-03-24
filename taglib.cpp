//go:build ignore
#include <cstdint>
#include <cstring>
#include <iostream>

#include "fileref.h"
#include "tpropertymap.h"
#include "mp4file.h"
#include "mp4tag.h"
#include "mp4item.h"

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

  // For MP4 files: also read freeform atoms (----:com.apple.iTunes:*)
  if (auto *mp4File = dynamic_cast<TagLib::MP4::File *>(file.file())) {
    if (auto *mp4Tag = mp4File->tag()) {
      for (const auto &item : mp4Tag->itemMap()) {
        TagLib::String key = item.first;
        if (key.startsWith("----:com.apple.iTunes:")) {
          // Extract short key from "----:com.apple.iTunes:KEYNAME"
          TagLib::String shortKey = key.substr(22); // len("----:com.apple.iTunes:") = 22
          if (!properties.contains(shortKey) && !shortKey.isEmpty()) {
            auto strList = item.second.toStringList();
            if (!strList.isEmpty()) {
              properties.insert(shortKey, strList);
            }
          }
        }
      }
    }
  }

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

  auto unsupported = file.setProperties(properties);

  // For MP4 files: write unsupported properties as freeform iTunes atoms
  // (----:com.apple.iTunes:KEYNAME). This allows custom tags like NARRATOR,
  // SERIES, PUBLISHER, etc. to persist in M4B/M4A files.
  if (!unsupported.isEmpty()) {
    if (auto *mp4File = dynamic_cast<TagLib::MP4::File *>(file.file())) {
      if (auto *mp4Tag = mp4File->tag()) {
        for (const auto &kvs : unsupported) {
          if (kvs.second.isEmpty())
            continue;
          TagLib::String atomKey = "----:com.apple.iTunes:" + kvs.first;
          mp4Tag->setItem(atomKey, TagLib::MP4::Item(kvs.second));
        }
      }
    }
  }

  return file.save();
}

struct FileProperties {
  uint32_t lengthInMilliseconds;
  uint32_t channels;
  uint32_t sampleRate;
  uint32_t bitrate;
  char **imageMetadata;
};

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
  props->bitrate = audioProperties->bitrate();

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
