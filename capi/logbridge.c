#include "logbridge.h"

void helmc_invoke_log_cb(helmc_log_callback cb, int32_t level,
                         const char* message, void* user_data) {
    cb(level, message, user_data);
}
