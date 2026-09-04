/*
 * Shared declarations for the end-to-end harness translation units.
 */
#ifndef HELMC_HARNESS_H
#define HELMC_HARNESS_H

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#ifndef _WIN32
#include <fcntl.h>
#include <pthread.h>
#include <sys/stat.h>
#include <unistd.h>
#endif

#include "helm_c.h"

extern int failures;

/* Chart directory produced by test_chart_authoring; later tests reuse it so
 * the harness needs no pre-existing chart fixture on disk. */
extern char* g_chart_dir;

#define CHECK(cond, msg)                                            \
    do {                                                            \
        if (!(cond)) {                                              \
            fprintf(stderr, "FAIL: %s (%s:%d)\n", msg, __FILE__, __LINE__); \
            failures++;                                             \
        }                                                           \
    } while (0)

/* Upper bound for library-returned strings when length-checking; keeps
 * string inspection bounded even if a returned buffer were unterminated
 * (CWE-126). */
#define HELMC_MAX_STR 4096

/* Free a library string and clear the variable: a failed call leaves its
 * out-param untouched, so a reused variable would otherwise hold a stale,
 * already-freed pointer. */
#define FREE_AND_NULL(p)        \
    do {                        \
        helm_free_string(p);    \
        (p) = NULL;             \
    } while (0)

/* Defined in additions.c: symbols added after 0.1.0. */
void test_additions(void);
void config_additions_checks(helm_handle_t cfg);

#endif /* HELMC_HARNESS_H */
