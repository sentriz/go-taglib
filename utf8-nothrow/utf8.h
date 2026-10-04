// Stands in for utfcpp so invalid text converts to an empty string, as TagLib's catch would, since the wasm build can't
// catch exceptions.
//
// TODO: delete when wazero's exception handling is stable and fast, and build with -fwasm-exceptions.

#ifndef GO_TAGLIB_UTF8_NOTHROW_H
#define GO_TAGLIB_UTF8_NOTHROW_H

#include <exception>

#include "../taglib/3rdparty/utfcpp/source/utf8/unchecked.h"

namespace utf8
{
  class exception : public std::exception {};

  template <typename u16bit_iterator, typename octet_iterator>
  u16bit_iterator utf8to16(octet_iterator start, octet_iterator end, u16bit_iterator result)
  {
    if(!utf8::is_valid(start, end))
      return result;
    return utf8::unchecked::utf8to16(start, end, result);
  }

  template <typename u16bit_iterator, typename octet_iterator>
  octet_iterator utf16to8(u16bit_iterator start, u16bit_iterator end, octet_iterator result)
  {
    for(u16bit_iterator it = start; it != end; ++it) {
      uint32_t cp = internal::mask16(*it);
      if(internal::is_trail_surrogate(cp))
        return result;
      if(internal::is_lead_surrogate(cp) && (++it == end || !internal::is_trail_surrogate(internal::mask16(*it))))
        return result;
    }
    return utf8::unchecked::utf16to8(start, end, result);
  }
}

#endif
