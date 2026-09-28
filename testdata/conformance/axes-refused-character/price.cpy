      *****************************************************************
      * The two records of the axes-refused-character entry.
      *
      * Five bytes each: a one-character currency mark, which is what
      * the layout tells them apart by, and four characters beside it.
      *****************************************************************
       01  PRICE-RECORD.
           05  PRICE-CURRENCY          PIC X(1).
           05  PRICE-AMOUNT            PIC X(4).
       01  NOTE-RECORD.
           05  NOTE-CURRENCY           PIC X(1).
           05  NOTE-TEXT               PIC X(4).
