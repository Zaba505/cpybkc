      *****************************************************************
      * The two records of the axes-refused-float entry.
      *
      * Eight bytes each: a COMP-1 value, which is what the layout tells
      * them apart by, and a four-byte note.
      *****************************************************************
       01  LIMIT-RECORD.
           05  LIMIT-VALUE             COMP-1.
           05  LIMIT-NOTE              PIC X(4).
       01  READING-RECORD.
           05  READING-VALUE           COMP-1.
           05  READING-NOTE            PIC X(4).
