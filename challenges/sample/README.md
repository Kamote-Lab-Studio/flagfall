# Sample challenge pack

The 12 sample challenges from the designs, as `challenge.yml` folders (see [docs/dev/challenge-format.md](../../docs/dev/challenge-format.md)). Loaded by `flagfall seed sample`. Every challenge is really solvable: the downloads are real files, the four networked challenges run real services, and each one has a reference solution that has been checked.

| Challenge | What players get |
|---|---|
| Sanity Check | the flag is on the rules page |
| Baby RSA, Lattice Whisper | `dist/`: the encryption script and its output |
| Packet Archaeology | `dist/capture.pcap` |
| Memory Lane | `dist/memory.raw.gz` (a 32 MiB image) |
| Where Is the Server | `dist/rack.jpg` |
| Crackme v3, Obfuscated Oracle | `dist/`: stripped static Linux binaries (plus the oracle's bytecode) |
| Cookie Jar, Template of Doom | a web service on ports 31080 and 31081 |
| Stack Smasher, Heap Feng Shui | a `nc` service on ports 31337 and 31338, plus the binary in `dist/` |

## Layout

Each folder can contain:

- `challenge.yml`, `description.md`, `hints/`: the challenge itself.
- `dist/`: files players download (listed under `files:`).
- `Dockerfile` and `service/`: the challenge service (the `service:` block in `challenge.yml`). Flagfall builds and runs it; see [docs/dev/challenge-services.md](../../docs/dev/challenge-services.md).
- `src/`: the C sources of the binaries.
- `solve/solve.py`: the reference solution, using only the Python standard library. The exceptions are Lattice Whisper, which needs `fpylll` (and `cysignals`), and Where Is the Server, which needs `pillow`.

Only the first three are built into Flagfall. `src/`, `solve/` and `tools/` stay in the repository, so players never get the solutions.

## Running the services

Flagfall only runs challenge services when it can reach Docker. Locally, that means starting the stack with the override:

```sh
docker compose -f compose.yaml -f compose.services.yaml up -d
```

Then start the services from the admin dashboard.

## Tools

Run these from the repository root. Both need Docker.

- `sh challenges/sample/tools/build-binaries.sh` regenerates everything. It runs `go run ./challenges/sample/tools/gen`, which writes the crypto, forensics and photo files and the reversing data headers, then compiles the four binaries in Docker. Musl builds use Alpine; Heap Feng Shui uses Ubuntu 24.04 for glibc 2.39. The generator is deterministic, seeded per challenge, so running it again gives the same files.
- `sh challenges/sample/tools/check-solves.sh` builds and starts the four services, runs every solve, and checks each printed flag against `challenge.yml`. It leaves nothing running.
