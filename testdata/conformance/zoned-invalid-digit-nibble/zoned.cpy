      *****************************************************************
      * One signed zoned item, whose second occurrence in the file is invalid.
      *
      * Five bytes. The byte 0x7B is a valid sign byte under no convention this
      * project carries: its low nibble is B, and a digit nibble is 0 to 9.
      *****************************************************************
       01  ZONED-RECORD.
           05  Z-SIGNED                PIC S9(5).
