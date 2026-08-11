      *****************************************************************
      * Packed decimal items covering Appendix A.4's positive rows.
      *
      * Twenty-one bytes: seven items of three each, because five digits and a
      * sign is six nibbles and four digits and a sign is five, which rounds up
      * to the same three bytes.
      *
      * P-SCALED is byte-identical to P-SIGNED-NEG. Scale is not stored, so the
      * implied decimal point of PIC S9(3)V99 costs no byte and is not
      * recoverable from the data.
      *****************************************************************
       01  PACKED-RECORD.
           05  P-SIGNED-POS            PIC S9(5) COMP-3.
           05  P-SIGNED-NEG            PIC S9(5) COMP-3.
           05  P-UNSIGNED              PIC 9(5) COMP-3.
           05  P-FOUR-NEG              PIC S9(4) COMP-3.
           05  P-FOUR-POS              PIC S9(4) COMP-3.
           05  P-SCALED                PIC S9(3)V99 COMP-3.
           05  P-LENIENT               PIC S9(5) COMP-3.
