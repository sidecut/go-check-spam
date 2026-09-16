#include <stddef.h>

__attribute__((weak)) void malloc_trim(size_t pad) {
    (void)pad;
}

__attribute__((weak)) int backtrace(void **buffer, int size) {
    (void)buffer;
    (void)size;
    return 0;
}

__attribute__((weak)) char **backtrace_symbols(void *const *buffer, int size) {
    (void)buffer;
    (void)size;
    return NULL;
}

__attribute__((weak)) int __res_init(void) {
    return 0;
}