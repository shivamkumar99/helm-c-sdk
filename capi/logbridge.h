/*
 * Internal C bridge for invoking host log callbacks from Go.
 * The public typedef mirrors helm_log_callback in include/helm_c.h — keep
 * the two in sync (ABI).
 */
#ifndef HELMC_LOGBRIDGE_H
#define HELMC_LOGBRIDGE_H

#include <stdint.h>

typedef void (*helmc_log_callback)(int32_t level, const char* message,
                                   void* user_data);

/* Defined in logbridge.c — cgo cannot define C functions in the preamble of
 * a file that uses //export. */
void helmc_invoke_log_cb(helmc_log_callback cb, int32_t level,
                         const char* message, void* user_data);

#endif /* HELMC_LOGBRIDGE_H */
