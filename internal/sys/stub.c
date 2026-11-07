#include <moonbit.h>
#include <sys/clonefile.h>

MOONBIT_FFI_EXPORT
int32_t ws_clonefile(moonbit_bytes_t source, moonbit_bytes_t destination) {
  return clonefile((const char *)source, (const char *)destination, 0);
}
