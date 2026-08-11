      *****************************************************************
      * Binary items covering Appendix A.5's first four rows.
      *
      * Ten bytes: three items of two and one of four. The width is a staircase
      * and not the digit count -- nine digits occupy four bytes, not nine.
      *****************************************************************
       01  BINARY-RECORD.
           05  B-SHORT-POS             PIC S9(4) COMP.
           05  B-SHORT-NEG             PIC S9(4) COMP.
           05  B-ZERO                  PIC S9(4) COMP.
           05  B-LONG                  PIC S9(9) COMP.
