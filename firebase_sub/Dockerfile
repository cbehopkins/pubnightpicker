FROM python:3.12-slim-bookworm

WORKDIR /usr/src/app

ENV POETRY_VERSION=1.8.3
ENV POETRY_VIRTUALENVS_CREATE=false

RUN apt-get update \
	&& apt-get -y upgrade \
	&& rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir "poetry==$POETRY_VERSION"

COPY pyproject.toml poetry.lock ./
RUN poetry install --only main --no-interaction --no-ansi --no-root

COPY . .

# Runsub events
# set housekeeping cron to run every Thursday at 12:00 PM
CMD [ "python", "-m", "firebase_sub.cli.sub_events", "--housekeeping-cron", "0 12 * * 4" ]
