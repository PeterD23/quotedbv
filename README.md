# QuoteDB Video Edition
This is a fork of the original QuoteDB, adding the ability to upload videos in the following containers and codecs:
- .mp4 (h264) .webm (h264, vp8, vp9)
Invalid videos will be rejected as they don't appear to be embeddable in a \<video\> tag.

## Changes to the original
- HTTP Session Management when uploading files, since the video upload runs in a separate form to the quote uploader a variable is needed to pass to the quote form struct.
- Uses TLS instead of regular HTTP so it requires a valid certificate in order to use CSRF. You can turn it off by disabling csrf.Secure but that is not recommended if you intend on exposing it to the net.

## Usage
Clone the repository, and you will want to build the Dockerfile with a docker-compose.yml. You will need a valid certificate.crt and private.key file if you want to use it in HTTPS, so either generate a self-signed cert or submit a CSR to a Certificate Authority.

The docker-compose.yml should be structured roughly like this:
```
services:
  quotedb:
    container_name: quotedb
    build: .
    restart: always
    volumes:
      - ./<quotefolder>:/<quotefolder>
      - ./<videofolder>:/<videofolder>
    ports:
      - 80:80
      - 443:10443
    entrypoint: /go/src/github.com/cj123/quotedb/quotedb -p <password> -f <quotefolder> -v <videofolder>
```
You will want to make sure the folders you bind are already present in the project folder, otherwise you may run into permissions issues.
The incorrect.html expects an mp4 in your videos folder called "incorrect.mp4" so feel free to add your own!

If you are hosting outside of Docker, you will want to have FFMpeg installed since the ValidCodec function is dependent on an ffprobe wrapper.


