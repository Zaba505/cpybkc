      *****************************************************************
      * The record that closes a batch.
      *
      * Sixteen bytes: the type code, the number of detail records the
      * batch carried as an unsigned zoned count, and filler out to the
      * width every record type of this file shares.
      *****************************************************************
       01  BATCH-TRAILER.
           05  TRL-TYPE                PIC X(1).
           05  TRL-COUNT               PIC 9(5).
           05  TRL-FILLER              PIC X(10).
