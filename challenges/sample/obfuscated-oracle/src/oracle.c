/* Obfuscated Oracle: a small stack machine runs check.vm against your input.
 * Built by challenges/sample/tools/build-binaries.sh (static, stripped). */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "ops.h"

#define STACK 64

static unsigned char code[4096];
static long code_len;

/* Each byte of the program is stored XOR'd with a key that depends on its
 * position. */
static unsigned char fetch(long pc) {
    if (pc < 0 || pc >= code_len) {
        puts("oracle: ran off the program");
        exit(2);
    }
    return code[pc] ^ (unsigned char)(0x5a + 13 * pc);
}

static int run(const unsigned char *in, size_t in_len) {
    unsigned char st[STACK];
    int sp = 0;
    long pc = 0;
#define NEED(n) if (sp < (n)) { puts("oracle: stack underflow"); exit(2); }
#define PUSHV(v) do { if (sp >= STACK) { puts("oracle: stack overflow"); exit(2); } st[sp++] = (unsigned char)(v); } while (0)
    for (;;) {
        unsigned char op = fetch(pc++);
        switch (op) {
        case OP_HALT: NEED(1); return st[sp - 1];
        case OP_PUSH: PUSHV(fetch(pc++)); break;
        case OP_POP: NEED(1); sp--; break;
        case OP_DUP: NEED(1); PUSHV(st[sp - 1]); break;
        case OP_SWAP: { NEED(2); unsigned char t = st[sp - 1]; st[sp - 1] = st[sp - 2]; st[sp - 2] = t; break; }
        case OP_ADD: NEED(2); sp--; st[sp - 1] = (unsigned char)(st[sp - 1] + st[sp]); break;
        case OP_SUB: NEED(2); sp--; st[sp - 1] = (unsigned char)(st[sp - 1] - st[sp]); break;
        case OP_XOR: NEED(2); sp--; st[sp - 1] ^= st[sp]; break;
        case OP_MUL: NEED(2); sp--; st[sp - 1] = (unsigned char)(st[sp - 1] * st[sp]); break;
        case OP_LOAD: { NEED(1); unsigned char i = st[--sp]; PUSHV(i < in_len ? in[i] : 0); break; }
        case OP_LEN: PUSHV(in_len); break;
        case OP_JMP: case OP_JZ: case OP_JNZ: {
            short rel = (short)(fetch(pc) | (fetch(pc + 1) << 8));
            pc += 2;
            int take = 1;
            if (op != OP_JMP) {
                NEED(1);
                unsigned char v = st[--sp];
                take = (op == OP_JZ) ? (v == 0) : (v != 0);
            }
            if (take) pc += rel;
            break;
        }
        default:
            printf("oracle: bad opcode 0x%02x at %ld\n", op, pc - 1);
            exit(2);
        }
    }
}

int main(int argc, char **argv) {
    const char *path = argc > 1 ? argv[1] : "check.vm";
    FILE *f = fopen(path, "rb");
    if (!f) {
        printf("usage: %s [check.vm]\n", argv[0]);
        return 2;
    }
    code_len = (long)fread(code, 1, sizeof code, f);
    fclose(f);

    char input[128];
    printf("The oracle is listening. Speak the flag: ");
    fflush(stdout);
    if (!fgets(input, sizeof input, stdin)) return 1;
    input[strcspn(input, "\r\n")] = 0;

    if (run((const unsigned char *)input, strlen(input)) == 1)
        puts("The oracle nods. That is the flag.");
    else
        puts("The oracle is silent.");
    return 0;
}
