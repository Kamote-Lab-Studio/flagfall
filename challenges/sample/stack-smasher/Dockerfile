# Stack Smasher: the login service from dist/smasher, one process per
# connection. Started by Flagfall from the service block in challenge.yml.
FROM alpine:3.20
RUN apk add --no-cache socat && adduser -D -h /home/ctf ctf
COPY dist/smasher /home/ctf/smasher
COPY service/flag.txt /home/ctf/flag.txt
RUN chmod 755 /home/ctf/smasher && chmod 444 /home/ctf/flag.txt
USER ctf
WORKDIR /home/ctf
EXPOSE 31337
CMD ["socat", "-T60", "TCP-LISTEN:31337,reuseaddr,fork", "EXEC:/home/ctf/smasher,stderr"]
