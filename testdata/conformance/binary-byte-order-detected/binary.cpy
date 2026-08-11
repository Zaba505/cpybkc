      *****************************************************************
      * One binary item of four digits, whose second occurrence in the file was
      * written big-endian into a file this layout declares little-endian.
      *
      * Two bytes: a binary item of four digits or fewer occupies two, and the
      * width is a staircase rather than the digit count.
      *****************************************************************
       01  BINARY-RECORD.
           05  B-VALUE                 PIC S9(4) COMP.
