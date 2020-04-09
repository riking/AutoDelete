#!/bin/sh

exit 1 # do not run this file

# shard 0 only
shard=0; cp ~/go/bin/autodelete ./autodelete.${shard?}; while true; do ./autodelete.${shard?} --shard=${shard?} 2>>error_log.${shard?}; sleep 5; done

# all other shards
shard=1; cp ~/go/bin/autodelete ./autodelete.${shard?}; while true; do ./autodelete.${shard?} --shard=${shard?} --nohttp 2>>error_log.${shard?}; sleep 5; done

