#include <moonbit.h>
#include <errno.h>
#include <stdint.h>
#include <sys/clonefile.h>
#include <sys/mount.h>
#include <sys/stat.h>

MOONBIT_FFI_EXPORT
int32_t ws_clonefile(moonbit_bytes_t source, moonbit_bytes_t destination) {
  return clonefile((const char *)source, (const char *)destination, 0);
}

// out: [種類(1=通常ファイル 2=ディレクトリ 3=symlink 0=その他), size, mode, dev, ino, ctime(ns)]
MOONBIT_FFI_EXPORT
int32_t ws_lstat(moonbit_bytes_t path, int64_t *out) {
  struct stat st;
  if (lstat((const char *)path, &st) != 0) return errno;
  out[0] = S_ISREG(st.st_mode) ? 1 : S_ISDIR(st.st_mode) ? 2 : S_ISLNK(st.st_mode) ? 3 : 0;
  out[1] = (int64_t)st.st_size;
  out[2] = (int64_t)(st.st_mode & 07777);
  out[3] = (int64_t)st.st_dev;
  out[4] = (int64_t)st.st_ino;
  out[5] = (int64_t)st.st_ctimespec.tv_sec * 1000000000 + st.st_ctimespec.tv_nsec;
  return 0;
}

MOONBIT_FFI_EXPORT
int64_t ws_available_bytes(moonbit_bytes_t path) {
  struct statfs fs;
  if (statfs((const char *)path, &fs) != 0) return -1;
  return (int64_t)fs.f_bavail * (int64_t)fs.f_bsize;
}
