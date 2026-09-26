// Stands in for utfcpp's <utf8.h> when building TagLib (see CMakeLists.txt).
//
// TagLib converts text with utfcpp's checked functions, which throw on invalid
// input, and catches the exception (taglib/toolkit/tstring.cpp). The wasm build
// has no exception support: the throw calls the __cxa_allocate_exception
// import, the host panics, and the whole call fails, so one badly encoded tag
// made a file impossible to read or write. TagLib only uses utf8to16,
// utf16to8 and utf8::exception from utfcpp, so this header provides those
// three, and neither conversion ever throws.
//
// Invalid input is decoded rather than dropped:
//   - UTF-8 that isn't valid is read as Windows-1252, which is what old taggers
//     wrote into Vorbis comments and UTF-8 frames. The whole string is decoded
//     that way, so every byte maps to exactly one character.
//   - UTF-16 surrogates that aren't part of a pair become U+FFFD.

#ifndef GO_TAGLIB_UTF8_NOTHROW_H
#define GO_TAGLIB_UTF8_NOTHROW_H

#include <exception>

#include "../taglib/3rdparty/utfcpp/source/utf8/unchecked.h"

namespace utf8
{
  // Never thrown, but TagLib's catch clauses name it.
  class exception : public ::std::exception {};

  namespace internal
  {
    // Windows-1252 bytes 0x80-0x9f. The five it leaves undefined keep their
    // Latin-1 value, as in the WHATWG mapping.
    const uint16_t cp1252_80_9f[32] = {
      0x20ac, 0x0081, 0x201a, 0x0192, 0x201e, 0x2026, 0x2020, 0x2021,
      0x02c6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008d, 0x017d, 0x008f,
      0x0090, 0x2018, 0x2019, 0x201c, 0x201d, 0x2022, 0x2013, 0x2014,
      0x02dc, 0x2122, 0x0161, 0x203a, 0x0153, 0x009d, 0x017e, 0x0178,
    };

    inline uint16_t cp1252_to_utf16(uint8_t b)
    {
      return (b >= 0x80 && b <= 0x9f) ? cp1252_80_9f[b - 0x80] : b;
    }
  } // namespace internal

  template <typename u16bit_iterator, typename octet_iterator>
  u16bit_iterator utf8to16(octet_iterator start, octet_iterator end, u16bit_iterator result)
  {
    if(utf8::is_valid(start, end))
      return utf8::unchecked::utf8to16(start, end, result);

    while(start != end)
      *result++ = internal::cp1252_to_utf16(internal::mask8(*start++));
    return result;
  }

  template <typename u16bit_iterator, typename octet_iterator>
  octet_iterator utf16to8(u16bit_iterator start, u16bit_iterator end, octet_iterator result)
  {
    while(start != end) {
      uint32_t cp = internal::mask16(*start++);
      if(internal::is_lead_surrogate(cp) && start != end &&
         internal::is_trail_surrogate(internal::mask16(*start)))
        cp = (cp << 10) + internal::mask16(*start++) + internal::SURROGATE_OFFSET;
      else if(internal::is_surrogate(cp))
        cp = 0xfffd;
      result = utf8::unchecked::append(cp, result);
    }
    return result;
  }
} // namespace utf8

#endif
