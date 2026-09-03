      *****************************************************************
      * The detail record of a batch.
      *
      * Forty bytes: an account number, a one-byte type code behind it,
      * an amount and filler. The type code is what the layout
      * discriminates on, and it sits at byte ten -- a different offset
      * and a different width from the header's, so the two runs share
      * no byte.
      *
      * BD-ACCOUNT is what covers bytes zero to two -- where a header
      * carries its own type code -- and it is PIC X, so no byte is
      * ruled out of it either.
      *****************************************************************
       01  BATCH-DETAIL.
           05  BD-ACCOUNT              PIC X(10).
           05  BD-TYPE                 PIC X(1).
           05  BD-AMOUNT               PIC 9(11).
           05  BD-FILLER               PIC X(18).
