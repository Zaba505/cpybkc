      *****************************************************************
      * The two records of the axes-no-charset-carried entry.
      *
      * Five bytes each: a one-byte tag the layout declares carries no
      * charset, and four characters of text. The tag is a byte a
      * transfer leaves alone; the text is characters it rewrites.
      *****************************************************************
       01  ALPHA-RECORD.
           05  ALPHA-TAG               PIC X(1).
           05  ALPHA-TEXT              PIC X(4).
       01  BETA-RECORD.
           05  BETA-TAG                PIC X(1).
           05  BETA-TEXT               PIC X(4).
