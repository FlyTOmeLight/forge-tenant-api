FROM ghcr.io/astral-sh/uv:0.12.21 AS uv
FROM golang:1.27.1-alpine
RUN apk add --no-cache python3 py3-pip git bash
COPY --from=uv /uv /usr/local/bin/uv
RUN uv venv --seed /opt/executor && uv pip install --python /opt/executor/bin/python pre-commit==4.6.2
WORKDIR /executor
COPY executor.py askpass.py /executor/
RUN chmod +x /executor/askpass.py
ENV PATH=/opt/executor/bin:$PATH PYTHONUNBUFFERED=1 WORKSPACE_ROOT=/workspace
ENTRYPOINT ["/opt/executor/bin/python","/executor/executor.py"]
