      *****************************************************************
      * The record that opens a batch.
      *
      * Forty bytes: a three-byte type code at the front, the batch's
      * number, its date, and filler out to the width both record types
      * of this file share. The type code is what the layout
      * discriminates on, and it sits at bytes zero to two.
      *
      * BH-DATE is what covers byte ten -- where a detail carries its own
      * type code -- and it is PIC X, so no byte is ruled out of it. That
      * is what leaves the pair to the order rather than to the
      * copybooks.
      *****************************************************************
       01  BATCH-HEADER.
           05  BH-TYPE                 PIC X(3).
           05  BH-BATCH-NO             PIC 9(6).
           05  BH-DATE                 PIC X(8).
           05  BH-FILLER               PIC X(23).
