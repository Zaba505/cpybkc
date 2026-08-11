      *****************************************************************
      * One signed zoned item, read under the Realia convention.
      *
      * Five bytes. Realia spells a negative sign as the byte 0x25, which is
      * an invalid zone under ascii-zone-37 -- the two conventions disagree
      * about this file and only one of them is right about it.
      *****************************************************************
       01  ZONED-RECORD.
           05  Z-SIGNED                PIC S9(5).
