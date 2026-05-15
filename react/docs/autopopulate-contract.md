# Autopopulate Contract

## Goal

When an admin clicks Autopopulate on an active poll, the system should add exactly 3 unique venues whenever at least 3 viable venues exist.

If fewer than 3 viable venues exist, the system should add all remaining viable venues and stop.

## Viable venue rules

A venue is viable only if all conditions are true:

- Not already present on the current poll.
- Not marked banned.
- Not visited within the recent exclusion window (currently 4 weeks).
- Venue type is pub.

## Candidate pools

Autopopulate computes candidates from three buckets:

- Most visited
- Least visited
- Random viable venues

The ranking sample window for most/least starts small and increases incrementally (in steps of 5 rows) until the top/bottom buckets expose at least 3 unique viable venues, or the ranked dataset is exhausted.

## Selection behavior

Selection is uniqueness-first:

1. Pick one unique venue from most visited.
2. Pick one unique venue from least visited.
3. Pick one unique venue from random.
4. If fewer than 3 unique venues were picked due to overlap, keep backfilling from remaining unique candidates across the pools.
5. Stop when 3 are selected, or when no additional unique viable venues remain.

## UX messaging

- Button tooltip should communicate "3 unique venues when possible".
- Success toast should report the actual count added.
- If zero venues are viable, show a no-viable-venues informational message.
