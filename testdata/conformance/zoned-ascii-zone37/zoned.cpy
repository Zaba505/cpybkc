      *****************************************************************
      * Zoned decimal items under one ASCII sign convention.
      *
      * Twenty-two bytes. A separate sign takes a byte of its own, which is
      * why the last two items are six bytes for five digits.
      *****************************************************************
       01  ZONED-RECORD.
           05  Z-SIGNED                PIC S9(5).
           05  Z-UNSIGNED              PIC 9(5).
           05  Z-LEAD-SEP              PIC S9(5) SIGN LEADING SEPARATE.
           05  Z-TRAIL-SEP             PIC S9(5) SIGN TRAILING SEPARATE.
