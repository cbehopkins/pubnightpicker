We use poetry install - so
poetry install

to create a venv
# Deisgn Overview




# Running/Building the docker
To build/run the docker n the pi:
cd ~/home/backup/pubnightpicker/firebase_sub
sudo docker build -t sub_events .

For debug:
`sudo docker run -it --rm --name pubnight_sub -e MAILTRAP_TOKEN="$MAILTRAP_TOKEN" sub_events`

Persist:
`sudo docker run -d --restart unless-stopped  --name pubnight_sub  -e MAILTRAP_TOKEN="$MAILTRAP_TOKEN" sub_events`

Check for stopped
`sudo docker ps -a`
and prune
`sudo docker container prune`
