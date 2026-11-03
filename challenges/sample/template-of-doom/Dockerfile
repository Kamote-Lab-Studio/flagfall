# Template of Doom: a Flask app with a server-side template injection.
# The flag is in /flag.txt; the app runs as an unprivileged user.
FROM python:3.12-alpine
RUN pip install --no-cache-dir flask==3.1.* && adduser -D app
COPY service/app.py /app/app.py
COPY service/flag.txt /flag.txt
RUN chmod 444 /flag.txt /app/app.py
USER app
WORKDIR /app
EXPOSE 8080
CMD ["python", "/app/app.py"]
