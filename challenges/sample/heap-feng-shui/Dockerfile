# Heap Feng Shui: the notes service from dist/notes on Ubuntu 24.04
# (glibc 2.39), one process per connection.
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends socat \
 && rm -rf /var/lib/apt/lists/* && useradd -m -d /home/ctf ctf
COPY dist/notes /home/ctf/notes
COPY service/flag.txt /home/ctf/flag.txt
RUN chmod 755 /home/ctf/notes && chmod 444 /home/ctf/flag.txt
USER ctf
WORKDIR /home/ctf
EXPOSE 31338
CMD ["socat", "-T120", "TCP-LISTEN:31338,reuseaddr,fork", "EXEC:/home/ctf/notes,stderr"]
