
## Satisfying a GDPR Data Request

 1. The request must include:
    - All guild IDs that data is requested for, and all channel IDs if the bot has been removed from the channel.
    - Proof that the requester has sufficient authority over the guild

    Use the identify_guilds script to check whether the requester has authority:
    `go run ~/go/src/github.com/riking/AutoDelete/cmd/identify_guilds.go -printroles $guildid`

 2. Pull all configuration .yml files from data/ in the bot storage directory for the identified guild:

        grep -l "$guildid" data/*.yml > /tmp/data_request_"$guildid"_channels
        grep -l "$guildid" data/*.yml | zip /tmp/data_request_"$guildid"_configs.zip -@

    If channel IDs are requested, add those to the list. Add all matched files to the response.
    Keep the list of all matched channel IDs for the next step.

 3. Perform the following Prometheus queries for all channel- and guild- specific information:

        joined_channel_ids="123|456"
        curl -o /tmp/data_request_"$guildid"_channeldata http://localhost:4000/api/v1/query?query=autodelete_message_reaps_by_channel%7Bchannel_id%3D~%22"${joined_channel_ids}"%22%7D%5B15d%5D
        curl -o /tmp/data_request_"$guildid"_guilddata http://localhost:4000/api/v1/query?query=autodelete_message_reaps_by_guild%7Bguild_id%3D~%22"${guildid}"%22%7D%5B15d%5D
        zip /tmp/data_request_"$guildid"_configs.zip /tmp/data_request_"$guildid"_channeldata /tmp/data_request_"$guildid"_guilddata

 4. Download and deliver the package:

        scp autodelete:/tmp/data_request_"$guildid"_configs.zip .

    Remember to delete it from your system after it's delivered!

## Canned response for not enough information

Hello, it appears that you have submitted a data access request. Because the bot does not store data associated with any particular user, you will need to prove that you have control over the guild in question.

Please provide the guild IDs in question.
