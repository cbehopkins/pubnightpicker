# Build stage: Create wheel from source
FROM python:3.12-slim-bookworm as builder

ENV POETRY_VERSION=2.1.3

RUN pip install --no-cache-dir "poetry==$POETRY_VERSION"

WORKDIR /build
COPY pyproject.toml poetry.lock ./
COPY firebase_sub ./firebase_sub

RUN poetry build --format wheel

# Runtime stage: Install dependencies and wheel
FROM python:3.12-slim-bookworm

WORKDIR /usr/src/app

ENV POETRY_VERSION=2.1.3
ENV POETRY_VIRTUALENVS_CREATE=false

RUN apt-get update \
	&& apt-get -y upgrade \
	&& rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir "poetry==$POETRY_VERSION"

COPY pyproject.toml poetry.lock ./
RUN poetry install --only main --no-interaction --no-ansi --no-root

COPY --from=builder /build/dist/*.whl .
RUN pip install *.whl && rm *.whl

# Run sub_events
# set housekeeping cron to run every Thursday at 12:00 PM
CMD [ "python", "-m", "firebase_sub.cli.sub_events", "--housekeeping-cron", "0 12 * * 4" ]
