      *****************************************************************
      * The record that opens a ledger extract.
      *
      * Twenty-four bytes: the type code in the first two, then what the
      * extract is of. Header and trailer are the two record types of
      * this file whose type code opens the record; a posting carries
      * its own behind the account key it shares with every other
      * posting, which is the point of the example.
      *****************************************************************
       01  LEDGER-HEADER.
           05  HDR-TYPE                PIC X(2).
           05  HDR-LEDGER-ID           PIC X(10).
           05  HDR-PERIOD              PIC 9(6).
           05  HDR-CURRENCY            PIC X(3).
           05  HDR-COUNT               PIC 9(3).
