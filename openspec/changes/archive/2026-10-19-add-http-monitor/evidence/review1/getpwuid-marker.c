// Diagnostic only: observe libc passwd lookup without changing its result.
#define _GNU_SOURCE
#include <dlfcn.h>
#include <pwd.h>
#include <stdio.h>
#include <sys/types.h>
#include <unistd.h>

int getpwuid_r(uid_t uid, struct passwd *pwd, char *buf, size_t len,
              struct passwd **result) {
    int (*next)(uid_t, struct passwd *, char *, size_t, struct passwd **) =
        dlsym(RTLD_NEXT, "getpwuid_r");
    if (next == NULL) _exit(125);
    fprintf(stderr, "diagnostic libc getpwuid_r pid=%ld uid=%ld\n", (long)getpid(), (long)uid);
    return next(uid, pwd, buf, len, result);
}
