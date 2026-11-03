/* Heap Feng Shui: a note-taking service with a use-after-free.
 * Built on Ubuntu 24.04 (glibc 2.39) by challenges/sample/tools/build-binaries.sh
 * with every protection: PIE, full RELRO, stack canaries, NX, FORTIFY. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define MAX_NOTES 8
#define MAX_SIZE 0x78

struct config {
    long admin; /* 0x1337 unlocks the admin console */
    char motd[24];
};

static struct config *cfg;
static char *notes[MAX_NOTES];
static size_t sizes[MAX_NOTES];

static void say(const char *s) {
    if (write(1, s, strlen(s)) < 0) exit(0);
}

static void read_exact(char *dst, size_t n) {
    size_t got = 0;
    while (got < n) {
        ssize_t r = read(0, dst + got, n - got);
        if (r <= 0) exit(0);
        got += (size_t)r;
    }
}

/* Reads a line and parses it as a number. */
static long read_num(const char *prompt) {
    char buf[32];
    size_t i = 0;
    say(prompt);
    while (i < sizeof buf - 1) {
        char c;
        if (read(0, &c, 1) != 1) exit(0);
        if (c == '\n') break;
        buf[i++] = c;
    }
    buf[i] = 0;
    return strtol(buf, NULL, 10);
}

static long pick(void) {
    long i = read_num("Index: ");
    if (i < 0 || i >= MAX_NOTES || !notes[i]) {
        say("No such note.\n");
        return -1;
    }
    return i;
}

int main(void) {
    setvbuf(stdin, NULL, _IONBF, 0);
    setvbuf(stdout, NULL, _IONBF, 0);
    cfg = malloc(sizeof *cfg);
    cfg->admin = 0;
    strcpy(cfg->motd, "No admin here. Bye!");

    say("=== Feng Shui Notes ===\n");
    for (;;) {
        say("\n1) New note  2) Delete note  3) Edit note  4) Show note  5) Admin console  6) Quit\n");
        long i;
        switch (read_num("> ")) {
        case 1: {
            i = read_num("Index: ");
            long size = read_num("Size: ");
            if (i < 0 || i >= MAX_NOTES || size <= 0 || size > MAX_SIZE) {
                say("Index 0-7, size 1-120.\n");
                break;
            }
            /* Not zeroed: malloc+memset would compile to calloc, which skips
             * the tcache. */
            notes[i] = malloc((size_t)size);
            sizes[i] = (size_t)size;
            say("Created.\n");
            break;
        }
        case 2:
            if ((i = pick()) < 0) break;
            free(notes[i]); /* the pointer stays in notes[] */
            say("Deleted.\n");
            break;
        case 3:
            if ((i = pick()) < 0) break;
            say("Content: ");
            read_exact(notes[i], sizes[i]);
            say("Saved.\n");
            break;
        case 4:
            if ((i = pick()) < 0) break;
            if (write(1, notes[i], sizes[i]) < 0) exit(0);
            say("\n");
            break;
        case 5:
            if (cfg->admin == 0x1337) {
                char flag[128] = {0};
                FILE *f = fopen("/home/ctf/flag.txt", "r");
                if (!f || !fgets(flag, sizeof flag, f)) {
                    say("flag.txt is missing: tell an organizer.\n");
                    break;
                }
                say("Admin console unlocked: ");
                say(flag);
            } else {
                say(cfg->motd);
                say("\n");
            }
            break;
        case 6:
            say("Bye.\n");
            return 0;
        default:
            say("?\n");
        }
    }
}
