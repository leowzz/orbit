#pragma once

#include <memory>

#include "orbit_display/protocol_codec.h"

std::unique_ptr<orbit_display::IProtocolCodec> makeNanopbProtocolCodec();
