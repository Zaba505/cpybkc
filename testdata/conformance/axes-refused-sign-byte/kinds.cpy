      *****************************************************************
      * The two records of the axes-refused-sign-byte entry.
      *
      * Five bytes each: a one-digit signed zoned kind and a four-byte
      * note. FIVE-KIND is told apart by A5, a zone an EBCDIC reader
      * admits as positive and no writer emits.
      *****************************************************************
       01  FIVE-RECORD.
           05  FIVE-KIND               PIC S9.
           05  FIVE-NOTE               PIC X(4).
       01  MINUS-RECORD.
           05  MINUS-KIND              PIC S9.
           05  MINUS-NOTE              PIC X(4).
