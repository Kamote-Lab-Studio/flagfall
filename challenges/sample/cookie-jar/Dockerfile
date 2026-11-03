# Cookie Jar: a small Python web app (standard library only).
FROM python:3.12-alpine
RUN adduser -D app
COPY service/app.py /app/app.py
COPY service/flag.txt /app/flag.txt
RUN chmod 444 /app/flag.txt /app/app.py
USER app
EXPOSE 8080
CMD ["python", "/app/app.py"]
