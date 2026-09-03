      *****************************************************************
      * The detail record of that batch.
      *
      * Forty bytes: a one-byte type code at the front, an account
      * number, an amount and filler. The type code is what the layout
      * discriminates on, and it sits at byte zero -- a different offset
      * and a different width from the header's, so the two runs share no
      * byte.
      *
      * BD-ACCOUNT is what covers bytes ten to twelve -- where a header
      * carries its own type code -- and it is an unsigned numeric
      * DISPLAY item, so no detail can carry the header's literal there
      * either. Both directions hold, which is what proves the pair
      * exclusive.
      *****************************************************************
       01  BATCH-DETAIL.
           05  BD-TYPE                 PIC X(1).
           05  BD-ACCOUNT              PIC 9(14).
           05  BD-AMOUNT               PIC 9(11).
           05  BD-FILLER               PIC X(14).
