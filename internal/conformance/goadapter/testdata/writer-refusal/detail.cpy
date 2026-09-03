      *****************************************************************
      * The detail record of that batch.
      *
      * Eight bytes: a packed amount at the front, a one-byte type code
      * behind it, and a reference. The type code is what the layout
      * discriminates on, and it sits at byte three, so the two runs
      * share no byte.
      *
      * BD-AMT is what covers bytes zero to two, where a header carries
      * its key. A packed item has a byte domain and the format declines
      * to claim one for it, so the copybooks prove nothing here and the
      * pair rests on the order.
      *****************************************************************
       01  BATCH-DETAIL.
           05  BD-AMT                  PIC S9(5) COMP-3.
           05  BD-TYPE                 PIC X(1).
           05  BD-REF                  PIC X(4).
