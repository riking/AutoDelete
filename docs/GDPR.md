
## Satisfying a GDPR Data Request

 1. The request must include:
    - All guild IDs that data is requested for, and all channel IDs if the bot has been removed from the channel.
    - Proof that the requester has sufficient authority over the guild (note: the `cmd/identify_guilds` script can be sufficient to prove this, if the request came over Discord)

 2. Pull all configuration .yml files from data/ in the bot storage directory for the identified guild:

        grep -l "$guild_id" data/*.yml

    If channel IDs are requested, add those to the list. Add all matched files to the response.
    Keep the list of all matched channel IDs for the next step.

 3. Perform a Prometheus query for
    `autodelete_message_reaps_by_guild{guild_id="..."}[15d]` and
    `autodelete_message_reaps_by_channel{channel_id=~"123|456|789"}[15d]` and
    deliver the results in the response by ... TODO: construct an API query so I can give them a file

This is all stored data.

