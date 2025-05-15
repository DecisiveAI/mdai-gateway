# Current changes
1. One `/events` route to rule them all
   2. returns audit stream on `GET`
   3. on `POST`
      4. processes request body
         - if request body is Alertmanager payload, converts to MdaiEvents
         - if request body is MdaiEvent, checks properties, fills as needed
         - if request body is neither, returns error
      5. processes MdaiEvent(s)
         - if we're keeping eventing, publishes each MdaiEvent to eventing channel
         - if we're _not_ keeping eventing, executes handler logic
      6. returns appropriate response
4. Refactor log structures -- normalized on structured logs via `zap`


## TODOs:
1. Ensure audit and debug logging is in place as appropriate
2. If we're keeping eventing, add retry logic to eventing connection
3. Version?