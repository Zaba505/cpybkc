      *****************************************************************
      * The record of the axes-refused-staircase entry.
      *
      * Six bytes under the 2-4-8 staircase: a two-digit binary count,
      * which is two bytes there and one under 1-2-4-8, and a four-byte
      * note behind it.
      *****************************************************************
       01  COUNT-RECORD.
           05  COUNT-VALUE             PIC S9(2) COMP.
           05  COUNT-NOTE              PIC X(4).
