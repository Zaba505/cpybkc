      *****************************************************************
      * The record that closes a ledger extract.
      *
      * Twenty-four bytes: the type code, the number of postings the
      * extract carried, the net of them as a packed decimal, and filler
      * out to the width the header shares.
      *
      * It is one of the two records selected on the field it opens
      * with. A posting is selected twelve bytes in, and two records
      * selected at two different offsets may not be admissible at the
      * same point -- see README.md, "Why the header counts the
      * postings", for what makes this file's automaton admit them at
      * different ones.
      *****************************************************************
       01  LEDGER-TRAILER.
           05  TRL-TYPE                PIC X(2).
           05  TRL-COUNT               PIC 9(6).
           05  TRL-NET                 PIC S9(13)V99 COMP-3.
           05  FILLER                  PIC X(8).
