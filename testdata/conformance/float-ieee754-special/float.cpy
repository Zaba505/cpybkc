      *****************************************************************
      * Four short floating-point items holding the values IEEE 754 has
      * and no other numeric encoding in this corpus does: a NaN, both
      * infinities, and a negative zero.
      *
      * Sixteen bytes. COMP-1 permits no PICTURE, so none of the four
      * carries one and every width comes with the usage.
      *****************************************************************
       01  FLOAT-RECORD.
           05  F-NAN                   COMP-1.
           05  F-POS-INF               COMP-1.
           05  F-NEG-INF               COMP-1.
           05  F-NEG-ZERO              COMP-1.
