      *****************************************************************
      * One signed zoned item, read under the translated-EBCDIC convention.
      *
      * Five bytes: the sign byte is the last one, and under this convention
      * it is the ASCII character an EBCDIC overpunch translates to.
      *****************************************************************
       01  ZONED-RECORD.
           05  Z-SIGNED                PIC S9(5).
