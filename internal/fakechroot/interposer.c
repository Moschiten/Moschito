#define _GNU_SOURCE
#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <stdarg.h>
#include <sys/stat.h>
#include <limits.h>
#include <errno.h>
#include <signal.h>
#include <ucontext.h>

#define MAX_PATH 4096

// 1. The SIGSYS Handler to bypass Android's seccomp filter
void sigsys_handler(int sig, siginfo_t *info, void *context) {
    ucontext_t *ctx = (ucontext_t *)context;
    
    // Set the return value to -ENOSYS (Function not implemented)
    // This forces glibc to fallback to older syscalls
    ctx->uc_mcontext.regs[0] = -ENOSYS;
    
    // Advance the Program Counter (PC) by 4 bytes to skip the SVC instruction
    // (On ARM64, syscalls are triggered by the SVC #0 instruction, which is 4 bytes)
    ctx->uc_mcontext.pc += 4;
}

__attribute__((constructor))
void init_handler() {
    write(2, "FAKECHROOT: Loaded successfully!\n", 33);
    struct sigaction sa;
    sa.sa_sigaction = sigsys_handler;
    sa.sa_flags = SA_SIGINFO;
    sigemptyset(&sa.sa_mask);
    sigaction(SIGSYS, &sa, NULL);
}

// 2. Path Rewriting Logic
const char* get_base() {
    return getenv("FAKECHROOT_BASE");
}

int is_excluded(const char *path) {
    if (!path) return 0;
    if (strncmp(path, "/proc", 5) == 0 || strncmp(path, "/sys", 4) == 0 || 
        strncmp(path, "/dev", 4) == 0 || strncmp(path, "/.moschito", 10) == 0) {
        return 1;
    }
    return 0;
}

void rewrite_path(const char *path, char *new_path) {
    const char *base = get_base();
    if (!base || !path || path[0] != '/' || is_excluded(path)) {
        if (!path) { new_path[0] = '\0'; return; }
        strncpy(new_path, path, MAX_PATH - 1);
        new_path[MAX_PATH - 1] = '\0';
        return;
    }
    snprintf(new_path, MAX_PATH, "%s%s", base, path);
}

int openat(int dirfd, const char *pathname, int flags, ...) {
    static int (*real_openat)(int, const char*, int, ...) = NULL;
    if (!real_openat) real_openat = dlsym(RTLD_NEXT, "openat");

    char new_path[MAX_PATH];
    rewrite_path(pathname, new_path);

    va_list args;
    va_start(args, flags);
    mode_t mode = va_arg(args, mode_t);
    va_end(args);

    if (flags & O_CREAT) return real_openat(dirfd, new_path, flags, mode);
    return real_openat(dirfd, new_path, flags);
}

int open(const char *pathname, int flags, ...) {
    static int (*real_open)(const char*, int, ...) = NULL;
    if (!real_open) real_open = dlsym(RTLD_NEXT, "open");

    char new_path[MAX_PATH];
    rewrite_path(pathname, new_path);

    va_list args;
    va_start(args, flags);
    mode_t mode = va_arg(args, mode_t);
    va_end(args);

    if (flags & O_CREAT) return real_open(new_path, flags, mode);
    return real_open(new_path, flags);
}

int chdir(const char *path) {
    static int (*real_chdir)(const char*) = NULL;
    if (!real_chdir) real_chdir = dlsym(RTLD_NEXT, "chdir");
    char new_path[MAX_PATH];
    rewrite_path(path, new_path);
    return real_chdir(new_path);
}

int execve(const char *pathname, char *const argv[], char *const envp[]) {
    static int (*real_execve)(const char*, char *const[], char *const[]) = NULL;
    if (!real_execve) real_execve = dlsym(RTLD_NEXT, "execve");
    char new_path[MAX_PATH];
    rewrite_path(pathname, new_path);
    return real_execve(new_path, argv, envp);
}

uid_t getuid(void) { return 0; }
uid_t geteuid(void) { return 0; }
gid_t getgid(void) { return 0; }
gid_t getegid(void) { return 0; }
