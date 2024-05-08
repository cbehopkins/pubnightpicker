To build/run the docker n the pi:
cd ~/home/backup/pubnightpicker/firebase_sub
sudo docker build -t sub_events .

For debug:
sudo docker run -it --rm --name pubnight_sub sub_events

Persist:
sudo docker run -d --restart unless-stopped  --name pubnight_sub sub_events