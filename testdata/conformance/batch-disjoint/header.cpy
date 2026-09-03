      *****************************************************************
      * The record that opens a batch, in the file whose copybooks prove
      * the pair exclusive.
      *
      * Forty bytes: the batch's number at the front, a three-byte type
      * code behind it, and filler out to the width both record types of
      * this file share. The type code is what the layout discriminates
      * on, and it sits at bytes ten to twelve.
      *
      * BH-BATCH-NO is what covers byte zero -- where a detail carries
      * its own type code -- and it is an unsigned numeric DISPLAY item,
      * so every byte of it is one of the charset's ten digit bytes. No
      * header can carry the detail's literal there.
      *****************************************************************
       01  BATCH-HEADER.
           05  BH-BATCH-NO             PIC 9(10).
           05  BH-TYPE                 PIC X(3).
           05  BH-FILLER               PIC X(27).
