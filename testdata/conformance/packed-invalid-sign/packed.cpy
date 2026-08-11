      *****************************************************************
      * One packed item of five digits, whose second occurrence in the file
      * carries a sign nibble that is not one.
      *
      * Three bytes: five digit nibbles and the sign nibble behind them.
      *****************************************************************
       01  PACKED-RECORD.
           05  P-VALUE                 PIC S9(5) COMP-3.
