/* SPDX-License-Identifier: GPL-2.0-or-later
 *
 * PCController's shared in-process / process-isolated libavrdude bridge.
 *
 * The adapter intentionally exposes a narrow, structured protocol instead of
 * AVRDUDE's terminal renderer. It links to the exact libavrdude source built
 * beside it and forwards callbacks directly, or as JSON Lines in helper mode.
 * Board access is still authorized and sequenced by the Go coordinator.
 */

#include <errno.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "libavrdude.h"
#include "config.h"
#include "bridge.h"

char *version = PCCONTROLLER_AVRDUDE_VERSION;
char *progname = "pccontroller-avrdude";
int verbose = 1;
int quell_progress = 0;
int ovsigck = 0;
const char *partdesc = "";
const char *pgmid = "";
libavrdude_context *cx;

static uintptr_t callback_user;
static pcc_avr_event event_callback;
static pcc_avr_cancelled cancel_callback;
static PROGRAMMER original_io;

static int cancelled(void) {
  return cancel_callback && cancel_callback(callback_user);
}

/* Cooperative cancellation at transfer boundaries. Never longjmp through
 * libavrdude, kill a CGO thread, or free state while a transport is active. */
static int guarded_paged_write(const PROGRAMMER *pgm, const AVRPART *p, const AVRMEM *m,
  unsigned int size, unsigned int addr, unsigned int n) {
  return cancelled()? -1: original_io.paged_write(pgm, p, m, size, addr, n);
}
static int guarded_paged_load(const PROGRAMMER *pgm, const AVRPART *p, const AVRMEM *m,
  unsigned int size, unsigned int addr, unsigned int n) {
  return cancelled()? -1: original_io.paged_load(pgm, p, m, size, addr, n);
}
static int guarded_write_byte(const PROGRAMMER *pgm, const AVRPART *p, const AVRMEM *m,
  unsigned long addr, unsigned char value) {
  return cancelled()? -1: original_io.write_byte(pgm, p, m, addr, value);
}
static int guarded_read_byte(const PROGRAMMER *pgm, const AVRPART *p, const AVRMEM *m,
  unsigned long addr, unsigned char *value) {
  return cancelled()? -1: original_io.read_byte(pgm, p, m, addr, value);
}

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
  if(event_callback) {
    event_callback(callback_user, "error", 0, message);
    return;
  }
  fputs("{\"type\":\"error\",\"message\":", stdout);
  json_string(stdout, message);
  fputs("}\n", stdout);
  fflush(stdout);
}

static void progress_callback(int percent, double elapsed, const char *header, int finish) {
  if(event_callback) {
    event_callback(callback_user, "progress", percent, header? header: "");
    return;
  }
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
  if(event_callback) {
    event_callback(callback_user, "message", level, buffer);
    return result;
  }
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
  const char *updates[16];
  size_t update_count;
  const char *fuses[4];
  size_t fuse_count;
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
      if(event_callback) {
        event_callback(callback_user, "version", 0, PCCONTROLLER_AVRDUDE_VERSION);
        return 1;
      }
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
    else if(strcmp(name, "--update") == 0) {
      if(options->update_count == sizeof options->updates / sizeof options->updates[0]) {
        emit_error("too many memory updates"); return -1;
      }
      options->updates[options->update_count++] = value;
    }
    else if(strcmp(name, "--read-fuse") == 0) {
      if(options->fuse_count == 4 || (strcmp(value, "lfuse") && strcmp(value, "hfuse") &&
        strcmp(value, "efuse") && strcmp(value, "lock"))) {
        emit_error("invalid fuse read"); return -1;
      }
      options->fuses[options->fuse_count++] = value;
    }
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
  if(!options->update_count && !options->chip_erase) {
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

typedef struct retained_allocation { void *pointer; int list; } retained_allocation;
static retained_allocation *retained;
static size_t retained_count, retained_capacity;

/* Part/programmer inheritance shares comments and caches. Collect first and
 * deduplicate before freeing: upstream object destructors intentionally leave
 * these shared objects to process exit. Never free a shared pointer twice. */
static void retain_for_cleanup(void *pointer, int list) {
  if(!pointer) return;
  if(retained_count == retained_capacity) {
    size_t capacity = retained_capacity? retained_capacity * 2: 1024;
    retained_allocation *next = realloc(retained, capacity * sizeof *next);
    if(!next) return; // Cleanup allocation failure may leak, never corrupt state.
    retained = next;
    retained_capacity = capacity;
  }
  retained[retained_count++] = (retained_allocation){pointer, list};
}
static void collect_strings(LISTID strings) {
  if(!strings) return;
  for(LNODEID n = lfirst(strings); n; n = lnext(n)) retain_for_cleanup(ldata(n), 0);
  retain_for_cleanup(strings, 1);
}
static void collect_comments(LISTID comments) {
  if(!comments) return;
  for(LNODEID n = lfirst(comments); n; n = lnext(n)) {
    COMMENT *c = ldata(n);
    if(c) {
      collect_strings(c->comms);
      retain_for_cleanup(c->kw, 0);
      retain_for_cleanup(c, 0);
    }
  }
  retain_for_cleanup(comments, 1);
}
static void collect_cache(AVR_Cache *cache) {
  if(!cache) return;
  retain_for_cleanup(cache->cont, 0);
  retain_for_cleanup(cache->copy, 0);
  retain_for_cleanup(cache->iscached, 0);
  retain_for_cleanup(cache, 0);
}
static int compare_allocation(const void *left, const void *right) {
  uintptr_t a = (uintptr_t)((const retained_allocation *)left)->pointer;
  uintptr_t b = (uintptr_t)((const retained_allocation *)right)->pointer;
  return (a > b) - (a < b);
}
static void collect_configuration(void) {
  // A failed parse can leave an unfinished object outside its owning list.
  // Modification syntax may instead point at an existing object: deduplicate.
  if(current_prog) {
    int found = 0;
    for(LNODEID n = lfirst(programmers); n; n = lnext(n)) if(ldata(n) == current_prog) found = 1;
    if(!found) ladd(programmers, current_prog);
  }
  if(current_part) {
    int found = 0;
    for(LNODEID n = lfirst(part_list); n; n = lnext(n)) if(ldata(n) == current_part) found = 1;
    if(!found) ladd(part_list, current_part);
  }
  for(LNODEID n = lfirst(part_list); n; n = lnext(n)) {
    AVRPART *p = ldata(n);
    collect_comments(p->comments);
    for(LNODEID m = p->mem? lfirst(p->mem): NULL; m; m = lnext(m)) collect_comments(((AVRMEM *)ldata(m))->comments);
  }
  for(LNODEID n = lfirst(programmers); n; n = lnext(n)) {
    PROGRAMMER *p = ldata(n);
    collect_comments(p->comments);
    collect_cache(p->cp_flash); collect_cache(p->cp_eeprom);
    collect_cache(p->cp_bootrow); collect_cache(p->cp_usersig);
  }
}

static void cleanup_context(void) {
  /* The CLI exits after one operation. A host must reclaim its parser cache
   * and update bookkeeping, and clear globals which refer into that cache. */
  avrdude_conf_version = default_programmer = default_parallel = "";
  default_serial = default_spi = default_linuxgpio = "";
  default_baudrate = 0;
  default_bitclock = 0;
  if(!cx) return;
  collect_strings(cx->cfg_prologue);
  collect_strings(cx->cfg_comms);
  collect_comments(cx->cfg_strctcomms);
  if(cx->cfg_pushed) collect_comments(cx->cfg_pushedcomms);
  qsort(retained, retained_count, sizeof *retained, compare_allocation);
  for(size_t i = 0; i < retained_count; ++i) {
    if(i && retained[i].pointer == retained[i-1].pointer) continue;
    if(retained[i].list) ldestroy(retained[i].pointer);
    else free(retained[i].pointer);
  }
  free(retained);
  retained = NULL;
  retained_count = retained_capacity = 0;
  free(cx->cfg_lkw);
  for(size_t i = 0; i < sizeof cx->cfg_hstrings / sizeof cx->cfg_hstrings[0]; ++i) {
    char **strings = cx->cfg_hstrings[i];
    if(strings) {
      for(size_t j = 0; strings[j]; ++j) free(strings[j]);
      free(strings);
    }
  }
  free(cx->upd_wrote);
  free(cx->upd_termcmds);
  free(cx);
  cx = NULL;
}

static int run_operation(int argc, char **argv) {
  adapter_options options;
  PROGRAMMER *programmer = NULL;
  AVRPART *part = NULL;
  UPDATE *update = NULL;
  UPDATE *updates[16] = {NULL};
  LISTID extended = NULL;
  const char *port;
  int opened = 0;
  int enabled = 0;
  int setup = 0;
  int status = 1;
  int initialized;
  long saved_recv_timeout = serial_recv_timeout;
  long saved_drain_timeout = serial_drain_timeout;
  struct serial_device *saved_serdev = serdev;

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
  // Do not reset cx here: configuration strings/comments are owned by it.
  programmer->flag = 0;
  cx->avr_disableffopt = 1; // Preserve trailing erased bytes in backups (-A).
  if(avr_initmem(part) != 0 || !programmer->initpgm) {
    emit_error("unable to initialize AVR memories or programmer functions");
    goto cleanup;
  }
  programmer->initpgm(programmer);
  original_io = *programmer;
  if(programmer->paged_write) programmer->paged_write = guarded_paged_write;
  if(programmer->paged_load) programmer->paged_load = guarded_paged_load;
  if(programmer->write_byte) programmer->write_byte = guarded_write_byte;
  if(programmer->read_byte) programmer->read_byte = guarded_read_byte;
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
  if(cancelled()) goto cleanup;
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
  if(cancelled()) goto cleanup;
  initialized = programmer->initialize? programmer->initialize(programmer, part): -1;
  /* Urclock's showall/showversion live in the library, not main.c. They emit
   * metadata through avrdude_message2 and return this successful early exit. */
  if(initialized == LIBAVRDUDE_EXIT_OK) {
    status = 0;
    goto cleanup;
  }
  if(initialized < 0) {
    emit_error("programmer could not initialize the selected AVR part");
    goto cleanup;
  }
  if(!signature_matches(programmer, part)) {
    emit_error("device signature does not match the selected AVR part");
    goto cleanup;
  }
  if(cancelled()) goto cleanup;
  for(size_t index = 0; index < options.fuse_count; ++index) {
    unsigned char value;
    char line[80];
    AVRMEM *memory = avr_locate_mem(part, options.fuses[index]);
    if(!memory || !programmer->read_byte ||
      programmer->read_byte(programmer, part, memory, 0, &value) < 0) {
      emit_error("could not read fuse metadata");
      goto cleanup;
    }
    snprintf(line, sizeof line, "0x%02x\n", value);
    avrdude_message2(stdout, 0, "", "", 0, 0, "%s", line);
  }
  if(options.chip_erase) {
    if(avr_chip_erase(programmer, part) != LIBAVRDUDE_SUCCESS) {
      emit_error("chip erase failed");
      goto cleanup;
    }
  }
  for(size_t index = 0; index < options.update_count; ++index) {
    if(cancelled()) goto cleanup;
    enum updateflags flags = UF_NOHEADING;
    update = parse_op(options.updates[index]);
    updates[index] = update;
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
  if(cancelled()) goto cleanup;
  if(programmer->flush_cache && programmer->flush_cache(programmer, part) < 0) {
    emit_error("libavrdude cache flush failed");
    goto cleanup;
  }
  if(programmer->end_programming && programmer->end_programming(programmer, part) < 0) {
    emit_error("programmer could not finish cleanly");
    goto cleanup;
  }
  status = 0;

cleanup:
  if(opened) {
    if(programmer->powerdown) programmer->powerdown(programmer);
    if(enabled && programmer->disable) programmer->disable(programmer);
    if(programmer->close) programmer->close(programmer);
  }
  if(setup && programmer->teardown) programmer->teardown(programmer);
  if(extended) ldestroy(extended);
  for(size_t index = 0; index < 16; ++index) if(updates[index]) free_update(updates[index]);
  collect_configuration();
  cleanup_config();
  current_prog = NULL;
  current_part = NULL;
  current_mem = NULL;
  part_list = NULL;
  programmers = NULL;
  cleanup_context();
  update_progress = NULL;
  serial_recv_timeout = saved_recv_timeout;
  serial_drain_timeout = saved_drain_timeout;
  serdev = saved_serdev;
  memset(&original_io, 0, sizeof original_io);
  if(cancelled()) status = 1;
  if(event_callback) event_callback(callback_user, "result", status == 0, "");
  else {
    fprintf(stdout, "{\"type\":\"result\",\"ok\":%s}\n", status == 0? "true": "false");
    fflush(stdout);
  }
  return status;
}

int pcc_avr_run(int argc, char **argv, uintptr_t user,
  pcc_avr_event event, pcc_avr_cancelled cancel) {
  callback_user = user;
  event_callback = event;
  cancel_callback = cancel;
  verbose = 1;
  quell_progress = ovsigck = 0;
  partdesc = pgmid = "";
  int result = cancelled()? 1: run_operation(argc, argv);
  event_callback = NULL;
  cancel_callback = NULL;
  callback_user = 0;
  return result;
}

#ifndef PCCONTROLLER_AVRDUDE_EMBEDDED
int main(int argc, char **argv) {
  return pcc_avr_run(argc, argv, 0, NULL, NULL);
}
#endif
