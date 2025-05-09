//go:build ignore
#include <cstring>
#include <iostream>

#include "fileref.h"
#include "tpropertymap.h"
#include "mpeg/mpegfile.h"
#include "mpeg/id3v1/id3v1tag.h"
#include "mpeg/id3v2/id3v2tag.h"
#include "mpeg/id3v2/frames/textidentificationframe.h"
#include "mpeg/id3v2/frames/commentsframe.h"
#include "mpeg/id3v2/frames/popularimeterframe.h"
#include "mpeg/id3v2/frames/unsynchronizedlyricsframe.h"

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

static const uint8_t CLEAR = 1 << 0;
static const uint8_t DIFF_SAVE = 1 << 1;

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

  if (opts & DIFF_SAVE) {
    if (file.properties() == properties)
      return true;
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

__attribute__((export_name("taglib_file_id3v2_frames"))) char **
taglib_file_id3v2_frames(const char *filename) {
  // First check if this is an MP3 file with ID3v2 tags
  TagLib::FileRef fileRef(filename);
  if (fileRef.isNull())
    return nullptr;
    
  // Try to cast to MPEG::File
  TagLib::MPEG::File *mpegFile = dynamic_cast<TagLib::MPEG::File *>(fileRef.file());
  if (!mpegFile || !mpegFile->hasID3v2Tag()) {
    // Return empty array instead of nullptr when there are no ID3v2 tags
    char **emptyFrames = static_cast<char **>(malloc(sizeof(char *)));
    if (!emptyFrames)
      return nullptr;
    emptyFrames[0] = nullptr;
    return emptyFrames;
  }
    
  TagLib::ID3v2::Tag *id3v2Tag = mpegFile->ID3v2Tag();
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

