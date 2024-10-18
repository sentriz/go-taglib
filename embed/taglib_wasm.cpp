#include "fileref.h"
#include "tag.h"
#include "tpropertymap.h"

#include <cstring>
#include <iostream>

extern "C" {

typedef struct {
  int dummy;
} TagLib_File;

TagLib_File *taglib_file_new(const char *filename) {
  return reinterpret_cast<TagLib_File *>(new TagLib::FileRef(filename));
}

bool taglib_file_is_valid(const TagLib_File *file) {
  return !reinterpret_cast<const TagLib::FileRef *>(file)->isNull();
}

char **taglib_file_tags(const TagLib_File *file) {
  auto f = reinterpret_cast<const TagLib::FileRef *>(file);
  auto properties = f->properties();

  size_t len = 0;
  for (const auto &kvs : properties)
    for (const auto &v : kvs.second)
      len++;

  char **tags = new char *[len + 1];
  size_t i = 0;
  for (const auto &kvs : properties)
    for (const auto &v : kvs.second) {
      TagLib::String row;
      row.append(kvs.first);
      row.append("\t");
      row.append(v);
      tags[i] = new char[row.size() + 1];
      strncpy(tags[i], row.toCString(), row.size());
      tags[i][row.size()] = '\0'; // Ensure null-termination
      i++;
    }

  tags[len] = NULL;
  return tags;
}

bool taglib_write_tags(TagLib_File *file, const char **tags) {
  auto f = reinterpret_cast<TagLib::FileRef *>(file);

  TagLib::PropertyMap properties;
  for (int i = 0; tags[i] != NULL; i++) {
    TagLib::String row(tags[i]);
    size_t tabPos = row.find("\t");
    if (tabPos != TagLib::String::npos) {
      TagLib::String key = row.substr(0, tabPos);
      TagLib::String value = row.substr(tabPos + 1);
      properties.insert(key, value);
    }
  }

  f->setProperties(properties);
  return f->save();
}

int *taglib_file_audioproperties(const TagLib_File *file) {
  auto f = reinterpret_cast<const TagLib::FileRef *>(file);

  int *arr = (int *)malloc(4 * sizeof(int));

  auto audioProperties = f->audioProperties();
  arr[0] = audioProperties->lengthInMilliseconds();
  arr[1] = audioProperties->channels();
  arr[2] = audioProperties->sampleRate();
  arr[3] = audioProperties->bitrate();

  return arr;
}

bool taglib_file_save(TagLib_File *file) {
  return reinterpret_cast<TagLib::FileRef *>(file)->save();
}

void taglib_file_free(TagLib_File *file) {
  delete reinterpret_cast<TagLib::FileRef *>(file);
}
}

int main() { return 0; }
