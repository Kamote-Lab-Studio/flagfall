# Sample challenge pack

The 12 sample challenges from the designs, as `challenge.yml` folders (see [docs/challenge-format.md](../../docs/challenge-format.md)). Loaded by `flagfall seed sample`. The downloads are real files, and the offline challenges have checked reference solutions.

For now the pack starts no containers. The four networked challenges have no `service:` block, so they can't be solved yet.

| Challenge | What players get |
|---|---|
| Sanity Check | the flag is on the rules page |
| Baby RSA, Lattice Whisper | `dist/`: the encryption script and its output |
| Packet Archaeology | `dist/capture.pcap` |
| Memory Lane | `dist/memory.raw.gz` (a 32 MiB image) |
| Where Is the Server | `dist/rack.jpg` |
| Crackme v3, Obfuscated Oracle | `dist/`: stripped static Linux binaries (plus the oracle's bytecode) |
| Cookie Jar, Template of Doom | the description only (no service yet) |
| Stack Smasher, Heap Feng Shui | the binary in `dist/` (no service yet) |

## Layout

Each folder can contain:

- `challenge.yml`, `description.md`, `hints/`: the challenge itself.
- `dist/`: files players download (listed under `files:`).
- `src/`: the C sources of the binaries.
- `solve/solve.py`: the reference solution, using only the Python standard library. The exceptions are Lattice Whisper, which needs `fpylll` (and `cysignals`), and Where Is the Server, which needs `pillow`. The four networked solves take a host and port.

Only the first two are built into Flagfall. `src/`, `solve/` and `tools/` stay in the repository, so players never get the solutions.

## Bringing the services back

Commit 7a22a8a has a working service for each networked challenge:

- the `Dockerfile` and `service/` folder;
- the `service:` and `connection:` lines in `challenge.yml`;
- a `check-solves.sh` that runs the network solves against them.

To restore them, check those files out of that commit and add `*/Dockerfile */service` back to the `//go:embed` line in `embed.go`. Then run the stack with `compose.services.yaml` (see [docs/challenge-services.md](../../docs/challenge-services.md)).

## Tools

Run these from the repository root. Both need Docker.

- `sh challenges/sample/tools/build-binaries.sh` regenerates everything. It runs `go run ./challenges/sample/tools/gen`, which writes the crypto, forensics and photo files and the reversing data headers, then compiles the four binaries in Docker. Musl builds use Alpine; Heap Feng Shui uses Ubuntu 24.04 for glibc 2.39. The generator is deterministic, seeded per challenge, so running it again gives the same files.
- `sh challenges/sample/tools/check-solves.sh` runs the offline solves and checks each printed flag against `challenge.yml`. It leaves nothing running.
