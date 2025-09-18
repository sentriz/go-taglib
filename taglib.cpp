//go:build ignore
#include <cstring>
#include <iostream>

#include "fileref.h"
#include "tpropertymap.h"

char *to_char_array(const TagLib::String &s) {
  const std::string str = s.to8Bit(true);
  return ::strdup(str.c_str());
}

TagLib::String to_string(const char *s) {
  return TagLib::String(s, TagLib::String::UTF8);
}

// size must come first so that we know how much of data to read
struct ByteData {
  unsigned int length;
  char *data;
};

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

__attribute__((export_name("taglib_file_audioproperties"))) int *
taglib_file_audioproperties(const char *filename) {
  TagLib::FileRef file(filename);
  if (file.isNull() || !file.audioProperties())
    return nullptr;

  int *arr = static_cast<int *>(malloc(4 * sizeof(int)));
  if (!arr)
    return nullptr;

  auto audioProperties = file.audioProperties();
  arr[0] = audioProperties->lengthInMilliseconds();
  arr[1] = audioProperties->channels();
  arr[2] = audioProperties->sampleRate();
  arr[3] = audioProperties->bitrate();

  return arr;
}

__attribute__((export_name("taglib_file_read_image"))) ByteData *
taglib_file_read_image(const char *filename) {
  TagLib::FileRef file(filename);
  if (file.isNull())
    return nullptr;

  const auto &pictures = file.complexProperties("PICTURE");
  if (pictures.isEmpty())
    return nullptr;

  ByteData *bd = (ByteData *)malloc(sizeof(ByteData));
  for (const auto &p : pictures) {
    const auto pictureType = p["pictureType"].toString();
    if (pictureType == "Front Cover") {
      auto v = p["data"].toByteVector();
      if (!v.isEmpty()) {
        bd->length = unsigned(v.size());
        bd->data = v.data();
        return bd;
      }
    }
  }

  // if we couldn't find a front cover, pick a random cover
  auto v = pictures.front()["data"].toByteVector();
  bd->length = unsigned(v.size());
  bd->data = v.data();
  return bd;
}

__attribute__((export_name("taglib_file_write_image"))) bool
taglib_file_write_image(const char *filename, const char *mimeType,
                        const char *buf, unsigned int length) {
  TagLib::FileRef file(filename);
  if (file.isNull())
    return false;

  if (length == 0) {
    if (!file.setComplexProperties("PICTURE", {}))
      return false;

    return file.save();
  }

  file.setComplexProperties("PICTURE",
                            {{{"data", TagLib::ByteVector(buf, length)},
                              {"pictureType", "Front Cover"},
                              {"mimeType", to_string(mimeType)},
                              {"description", "Added by go-taglib"}}});

  return file.save();
}
