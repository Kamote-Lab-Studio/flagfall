/* Stack Smasher: a tiny login service with a 64-byte buffer and no canary.
 * Built by challenges/sample/tools/build-binaries.sh:
 *   gcc -O0 -static -no-pie -fno-stack-protector (symbols kept). */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

void win(void) {
    char flag[128] = {0};
    FILE *f = fopen("/home/ctf/flag.txt", "r");
    if (!f) {
        puts("flag.txt is missing: tell an organizer.");
        exit(1);
    }
    fgets(flag, sizeof flag, f);
    printf("Welcome back, admin. %s", flag);
    exit(0);
}

static void login(void) {
    char username[64];
    puts("=== SmashCorp remote login ===");
    printf("Username: ");
    read(0, username, 256); /* oops */
    if (strncmp(username, "admin", 5) == 0)
        puts("Admins log in from the console, not over the network.");
    else
        puts("Access denied.");
}

int main(void) {
    setvbuf(stdout, NULL, _IONBF, 0);
    login();
    puts("Bye.");
    return 0;
}
