      *****************************************************************
      * The two records of the axes-zoned-sign-column entry.
      *
      * Five bytes each: a one-digit signed zoned kind, whose one byte
      * carries the overpunched sign, and a four-byte note. FIVE-KIND
      * holds +5 in either of the two spellings an EBCDIC reader admits;
      * MINUS-KIND holds -5.
      *****************************************************************
       01  FIVE-RECORD.
           05  FIVE-KIND               PIC S9.
           05  FIVE-NOTE               PIC X(4).
       01  MINUS-RECORD.
           05  MINUS-KIND              PIC S9.
           05  MINUS-NOTE              PIC X(4).
