FROM python:3.12-alpine
WORKDIR /app
COPY server.py rules.json ./
RUN adduser -D -u 1000 mock && mkdir -p /data && chown mock /data
USER mock
EXPOSE 11434
HEALTHCHECK --interval=5s --timeout=3s --retries=10 CMD wget -q -O /dev/null http://127.0.0.1:11434/health || exit 1
CMD ["python", "server.py"]
