/* SPDX-License-Identifier: GPL-2.0-or-later
 *
 * PCController's process-isolated libavrdude adapter.
 *
 * The adapter intentionally exposes a narrow, structured protocol instead of
 * AVRDUDE's terminal renderer. It links to the exact libavrdude source built
 * beside it and maps library callbacks to one JSON object per stdout line.
 * Board access is still authorized and sequenced by the Go coordinator.
 */

#include <errno.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "libavrdude.h"

char *version = PCCONTROLLER_AVRDUDE_VERSION;
char *progname = "pccontroller-avrdude";
int verbose = 1;
int quell_progress = 0;
int ovsigck = 0;
const char *partdesc = "";
const char *pgmid = "";
libavrdude_context *cx;

static void json_string(FILE *stream, const char *value) {
  const unsigned char *cursor = (const unsigned char *)(value ? value : "");
  fputc('"', stream);
  for(; *cursor; ++cursor) {
    switch(*cursor) {
    case '"': fputs("\\\"", stream); break;
    case '\\': fputs("\\\\", stream); break;
    case '\b': fputs("\\b", stream); break;
    case '\f': fputs("\\f", stream); break;
    case '\n': fputs("\\n", stream); break;
    case '\r': fputs("\\r", stream); break;
    case '\t': fputs("\\t", stream); break;
    default:
      if(*cursor < 0x20U)
        fprintf(stream, "\\u%04x", (unsigned int)*cursor);
      else
        fputc((int)*cursor, stream);
    }
  }
  fputc('"', stream);
}

static void emit_error(const char *message) {
  fputs("{\"type\":\"error\",\"message\":", stdout);
  json_string(stdout, message);
  fputs("}\n", stdout);
  fflush(stdout);
}

static void progress_callback(int percent, double elapsed, const char *header, int finish) {
  fputs("{\"type\":\"progress\",\"percent\":", stdout);
  fprintf(stdout, "%d,\"elapsed_seconds\":%.3f,\"detail\":", percent, elapsed);
  json_string(stdout, header);
  fprintf(stdout, ",\"finish\":%d}\n", finish);
  fflush(stdout);
}

int avrdude_message2(FILE *stream, int line, const char *file, const char *function,
  int mode, int level, const char *format, ...) {
  char buffer[4096];
  va_list args;
  int result;
  (void)stream;
  (void)line;
  (void)file;
  (void)function;
  (void)mode;
  va_start(args, format);
  result = vsnprintf(buffer, sizeof buffer, format, args);
  va_end(args);
  fputs("{\"type\":\"message\",\"level\":", stdout);
  fprintf(stdout, "%d,\"message\":", level);
  json_string(stdout, buffer);
  fputs("}\n", stdout);
  fflush(stdout);
  return result;
}

typedef struct adapter_options {
  const char *config;
  const char *part;
  const char *programmer;
  const char *port;
  const char *update;
  int baud;
  double bitclock_us;
  bool chip_erase;
  bool auto_erase;
  const char *extended[16];
  size_t extended_count;
} adapter_options;

static int parse_integer(const char *text, int *value) {
  char *end = NULL;
  long parsed;
  errno = 0;
  parsed = strtol(text, &end, 10);
  if(errno || !end || *end || parsed <= 0 || parsed > 2147483647L)
    return -1;
  *value = (int)parsed;
  return 0;
}

static int parse_double(const char *text, double *value) {
  char *end = NULL;
  double parsed;
  errno = 0;
  parsed = strtod(text, &end);
  if(errno || !end || *end || parsed <= 0.0)
    return -1;
  *value = parsed;
  return 0;
}

static int parse_arguments(int argc, char **argv, adapter_options *options) {
  memset(options, 0, sizeof *options);
  options->auto_erase = true;
  for(int index = 1; index < argc; ++index) {
    const char *name = argv[index];
    if(strcmp(name, "--version-json") == 0) {
      printf("{\"type\":\"version\",\"adapter\":\"1\",\"libavrdude\":\"%s\"}\n",
        PCCONTROLLER_AVRDUDE_VERSION);
      return 1;
    }
    if(strcmp(name, "--chip-erase") == 0) {
      options->chip_erase = true;
      continue;
    }
    if(strcmp(name, "--no-auto-erase") == 0) {
      options->auto_erase = false;
      continue;
    }
    if(index + 1 >= argc) {
      emit_error("missing option value");
      return -1;
    }
    const char *value = argv[++index];
    if(strcmp(name, "--config") == 0) options->config = value;
    else if(strcmp(name, "--part") == 0) options->part = value;
    else if(strcmp(name, "--programmer") == 0) options->programmer = value;
    else if(strcmp(name, "--port") == 0) options->port = value;
    else if(strcmp(name, "--update") == 0) options->update = value;
    else if(strcmp(name, "--baud") == 0 && parse_integer(value, &options->baud) != 0) {
      emit_error("invalid baud rate"); return -1;
    } else if(strcmp(name, "--bitclock-us") == 0 && parse_double(value, &options->bitclock_us) != 0) {
      emit_error("invalid bit-clock period"); return -1;
    } else if(strcmp(name, "--extended") == 0) {
      if(options->extended_count == sizeof options->extended / sizeof options->extended[0]) {
        emit_error("too many extended parameters"); return -1;
      }
      options->extended[options->extended_count++] = value;
    } else if(strcmp(name, "--baud") != 0 && strcmp(name, "--bitclock-us") != 0) {
      emit_error("unknown option"); return -1;
    }
  }
  if(!options->config || !options->part || !options->programmer) {
    emit_error("--config, --part and --programmer are required");
    return -1;
  }
  if(!options->update && !options->chip_erase) {
    /* No memory operation is a guarded probe. */
  }
  return 0;
}

static int signature_matches(PROGRAMMER *programmer, AVRPART *part) {
  AVRMEM *signature;
  if(avr_signature(programmer, part) != LIBAVRDUDE_SUCCESS)
    return 0;
  signature = avr_locate_signature(part);
  return signature && signature->size >= 3 &&
    memcmp(signature->buf, part->signature, 3) == 0;
}

int main(int argc, char **argv) {
  adapter_options options;
  PROGRAMMER *programmer = NULL;
  AVRPART *part = NULL;
  UPDATE *update = NULL;
  LISTID extended = NULL;
  const char *port;
  int opened = 0;
  int enabled = 0;
  int setup = 0;
  int status = 1;

  int parsed = parse_arguments(argc, argv, &options);
  if(parsed != 0)
    return parsed > 0 ? 0 : 2;

  update_progress = progress_callback;
  init_cx(NULL);
  if(init_config() != 0 || read_config(options.config) != 0) {
    emit_error("unable to load the pinned libavrdude configuration");
    goto cleanup;
  }
  part = locate_part(part_list, options.part);
  programmer = locate_programmer(programmers, options.programmer);
  if(!part || !programmer || !is_programmer(programmer)) {
    emit_error("configured AVR part or programmer was not found");
    goto cleanup;
  }
  if(!(part->prog_modes & programmer->prog_modes)) {
    emit_error("programmer and AVR part have no common programming mode");
    goto cleanup;
  }
  init_cx(programmer);
  if(avr_initmem(part) != 0 || !programmer->initpgm) {
    emit_error("unable to initialize AVR memories or programmer functions");
    goto cleanup;
  }
  programmer->initpgm(programmer);
  if(programmer->setup) {
    programmer->setup(programmer);
    setup = 1;
  }
  if(options.baud > 0) programmer->baudrate = options.baud;
  if(options.bitclock_us > 0.0) programmer->bitclock = options.bitclock_us * 0.000001;
  if(options.extended_count > 0) {
    extended = lcreat(NULL, 0);
    for(size_t index = 0; index < options.extended_count; ++index)
      ladd(extended, (void *)options.extended[index]);
    if(!programmer->parseextparams || programmer->parseextparams(programmer, extended) < 0) {
      emit_error("programmer rejected an extended parameter");
      goto cleanup;
    }
  }
  port = options.port;
  if(!port || !*port) {
    if(programmer->default_port && *programmer->default_port) port = programmer->default_port;
    else if(programmer->conntype == CONNTYPE_USB) port = "usb";
  }
  if(!port || !*port || !programmer->open || programmer->open(programmer, port) < 0) {
    emit_error("unable to open the selected programmer port");
    goto cleanup;
  }
  opened = 1;
  if(programmer->enable) {
    programmer->enable(programmer, part);
    enabled = 1;
  }
  if(!programmer->initialize || programmer->initialize(programmer, part) < 0) {
    emit_error("programmer could not initialize the selected AVR part");
    goto cleanup;
  }
  if(!signature_matches(programmer, part)) {
    emit_error("device signature does not match the selected AVR part");
    goto cleanup;
  }
  if(options.chip_erase) {
    if(avr_chip_erase(programmer, part) != LIBAVRDUDE_SUCCESS) {
      emit_error("chip erase failed");
      goto cleanup;
    }
  } else if(options.update) {
    enum updateflags flags = UF_NOHEADING;
    update = parse_op(options.update);
    if(!update || update_dryrun(part, update) != 0) {
      emit_error("invalid or inaccessible memory update");
      goto cleanup;
    }
    if(update->op == DEVICE_WRITE) {
      flags = (enum updateflags)(flags | UF_VERIFY);
      if(options.auto_erase) flags = (enum updateflags)(flags | UF_AUTO_ERASE);
    }
    if(do_op(programmer, part, update, flags) != 0) {
      emit_error("libavrdude memory operation failed");
      goto cleanup;
    }
  }
  if(programmer->flush_cache && programmer->flush_cache(programmer, part) < 0) {
    emit_error("libavrdude cache flush failed");
    goto cleanup;
  }
  if(programmer->end_programming && programmer->end_programming(programmer, part) < 0) {
    emit_error("programmer could not finish cleanly");
    goto cleanup;
  }
  fputs("{\"type\":\"result\",\"ok\":true}\n", stdout);
  fflush(stdout);
  status = 0;

cleanup:
  if(opened) {
    if(programmer->powerdown) programmer->powerdown(programmer);
    if(enabled && programmer->disable) programmer->disable(programmer);
    if(programmer->close) programmer->close(programmer);
  }
  if(setup && programmer->teardown) programmer->teardown(programmer);
  if(extended) ldestroy(extended);
  if(update) free_update(update);
  cleanup_config();
  return status;
}
