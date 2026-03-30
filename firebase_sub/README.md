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

Check for stopped
`sudo docker ps -a`
and prune
`sudo docker container prune`
