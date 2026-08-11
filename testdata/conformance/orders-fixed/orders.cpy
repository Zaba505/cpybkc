      *****************************************************************
      * The record of the orders-fixed conformance entry.
      *
      * Thirteen bytes: a four-byte key, a three-digit unsigned count
      * stored as characters, and a table of two occurrences of a
      * three-byte item. Nothing here is edited, signed, packed or
      * binary, so the bytes of a record are readable in a hex dump and
      * the entry is about the shape of a corpus entry rather than about
      * a byte-level rule.
      *****************************************************************
       01  ORDER-RECORD.
           05  ORDER-ID                PIC X(4).
           05  ORDER-QTY               PIC 9(3).
           05  ORDER-LINE OCCURS 2 TIMES.
               10  LINE-SKU            PIC X(3).
