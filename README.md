# besøg backend

## how to run

podman build -t dai-api .
podman run -p 8080:8080 --env-file .env -v ./data.db:/app/data.db localhost/dai-api:latest

## vision

En hjemmeside til konsulent rapport

rapport from nem og klikke
vedhæft billeder
lokation

integration til AdvoPro, hvilket betyder upload pdf

lang tid bliver brugt til at pakke en rapport.

personlig statestik
hvor mange biler har man fået ind
hvor mange besøg har man lavet.
en smule gamification

# TODO

use cycle

konsulent besøger hjemmeside
logger ind
kigger på sin dag og besøg
klikker på den første og åbner en questionaire som er adaptiv
trykker send, hvor både data og tidspunkt og person-placering bliver sendt med,
trykker videre på næste sag og gentager

admin besøger hjemmesiden
placerer besøg som skal findes enten igennem excel eller noget andet i fremtiden
tjekker på besøgs historik på alle konsulenter
trækker besøgs data ud og skriver ind i advopro (måske automatisk i fremtiden)

## done

login
register
besøgs form
Hvis konsulenten har tjekket at bilen er skadet så skal han også vedhæfte et billede

## currently

### Fix deployment

podman
automated deployment

### Add functionanility

## future

typer af besøg (anmeldt) (uanmeldt genbesøg: indenfor 10 dage af seneste anmeldte besøg) (anmeldt genbesøg)

self host nominatim for long og lattitude
