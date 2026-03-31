We use Poetry for local development dependencies:
poetry install

For runtime-only dependencies (for lean environments):
poetry install --only main

to create a venv

# Test with
`poetry run tox -r`

For debugging against the firestore emulator, in the vscode env setting:

`"FIRESTORE_EMULATOR_HOST": "127.0.0.1:8080",`

For VS Code Test Explorer runs, env vars are loaded from `.env.test` via `.vscode/settings.json`.
Current defaults:

`FIRESTORE_EMULATOR_HOST=127.0.0.1:8080`

`GOOGLE_CLOUD_PROJECT=demo-firebase-sub-integration`



# Deisgn Overview




# Running/Building the docker
To build/run the docker on the pi:
cd ~/home/git/src/github.com/cbehopkins/pubnightpicker
sudo docker build -t sub_events .

For debug:
`sudo docker run -it --rm --name pubnight_sub -e MAILTRAP_TOKEN="$MAILTRAP_TOKEN" sub_events`

Persist:
`sudo docker run -d --restart unless-stopped  --name pubnight_sub  -e MAILTRAP_TOKEN="$MAILTRAP_TOKEN" sub_events`

# Restarting the deployed service (compose repo)
If you deploy with the separate compose repo, this is the low-downtime flow we used:

1. Build a fresh local image on the same Docker host that runs the service:

```bash
cd .../github.com/cbehopkins/pubnightpicker/firebase_sub
sudo docker build -t sub_events .
```

2. Recreate only the `pubnight_sub` service from the compose repo (This is my private compose repo - no peeking):

```bash
cd .../home_compose/pubnight_sub
sudo docker compose up -d --no-deps --force-recreate pubnight_sub
```

3. Verify status and logs:

```bash
sudo docker compose ps pubnight_sub
sudo docker compose logs --tail 100 -f pubnight_sub
```

If you see `container name "/pubnight_sub" is already in use`, remove the old manually-run container and retry:

```bash
sudo docker rm -f pubnight_sub
sudo docker compose up -d --no-deps --force-recreate pubnight_sub
```

This conflict happens when an older container with the same name exists but was not created by Docker Compose.

Check for stopped
`sudo docker ps -a`
and prune
`sudo docker container prune`
