# D5 clock floor preflight (partial, not acceptance)

The new7.1 function now uses a single fresh DB clock observation and the maximum
of original Run, committed physical release and registered Worker time floors.
>2s divergence is clock_untrusted; no physical permission or Lease is extended.
Normal initial real PG18/Linux cases pass, including1s/10s stored-time skew
checks. These are private fixture marker variations, not actual host-clock
changes or mocked network/Supervisor proof. The full24-cell normal/race matrix
and exact final SHA CI are pending here; later final acceptance supersedes this
partial snapshot.7.1 remains unchecked. Original negative evidence retained.
