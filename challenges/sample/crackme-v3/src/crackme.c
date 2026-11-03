/* Crackme v3: enter the right serial and the binary prints the flag.
 * Built by challenges/sample/tools/build-binaries.sh (static, stripped). */
#include <stdio.h>
#include <string.h>

#include "data.h"

static unsigned char rol1(unsigned char v) { return (unsigned char)((v << 1) | (v >> 7)); }

int main(void) {
    char serial[64];
    printf("crackme v3 -- licensed to: you\nserial: ");
    fflush(stdout);
    if (!fgets(serial, sizeof serial, stdin)) return 1;
    serial[strcspn(serial, "\r\n")] = 0;

    if (strlen(serial) != SERIAL_LEN) {
        puts("Invalid serial.");
        return 1;
    }
    unsigned char acc = 0x5a;
    for (int i = 0; i < SERIAL_LEN; i++) {
        acc = rol1(acc) ^ (unsigned char)serial[i] ^ TABLE[i % 16];
        if (acc != EXPECT[i]) {
            puts("Invalid serial.");
            return 1;
        }
    }

    char flag[FLAG_LEN + 1];
    for (int i = 0; i < FLAG_LEN; i++)
        flag[i] = (char)(ENC[i] ^ (unsigned char)serial[i % SERIAL_LEN] ^ (unsigned char)(i * 7));
    flag[FLAG_LEN] = 0;
    printf("Serial accepted. %s\n", flag);
    return 0;
}
