#include <moonbit.h>

#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <spawn.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/clonefile.h>
#include <unistd.h>

extern char **environ;

MOONBIT_FFI_EXPORT
int32_t ws_clonefile(moonbit_bytes_t source, moonbit_bytes_t destination) {
  if (clonefile((const char *)source, (const char *)destination, 0) == 0) return 0;
  return errno != 0 ? errno : 1;
}

MOONBIT_FFI_EXPORT
int32_t ws_getpid(void) { return (int32_t)getpid(); }

// argv_blob は NUL 終端文字列を argc 個連結したもの
MOONBIT_FFI_EXPORT
int32_t ws_spawn_detached(moonbit_bytes_t argv_blob, int32_t argc) {
  char **argv = calloc((size_t)argc + 1, sizeof(char *));
  if (argv == NULL) return ENOMEM;
  char *cursor = (char *)argv_blob;
  for (int32_t i = 0; i < argc; ++i) {
    argv[i] = cursor;
    cursor += strlen(cursor) + 1;
  }

  posix_spawnattr_t attr;
  posix_spawnattr_init(&attr);
  // async ランタイムはスレッドのシグナルをブロックしており、そのまま継承すると子が SIGINT/SIGTERM で終了しなくなる
  sigset_t empty_mask;
  sigemptyset(&empty_mask);
  sigset_t all_signals;
  sigfillset(&all_signals);
  posix_spawnattr_setsigmask(&attr, &empty_mask);
  posix_spawnattr_setsigdefault(&attr, &all_signals);
  posix_spawnattr_setflags(&attr, POSIX_SPAWN_SETSID | POSIX_SPAWN_SETSIGMASK | POSIX_SPAWN_SETSIGDEF);

  posix_spawn_file_actions_t actions;
  posix_spawn_file_actions_init(&actions);
  posix_spawn_file_actions_addopen(&actions, 0, "/dev/null", O_RDONLY, 0);
  posix_spawn_file_actions_addopen(&actions, 1, "/dev/null", O_WRONLY, 0);
  posix_spawn_file_actions_addopen(&actions, 2, "/dev/null", O_WRONLY, 0);

  pid_t pid;
  int result = posix_spawnp(&pid, argv[0], &actions, &attr, argv, environ);

  posix_spawn_file_actions_destroy(&actions);
  posix_spawnattr_destroy(&attr);
  free(argv);
  return result;
}
