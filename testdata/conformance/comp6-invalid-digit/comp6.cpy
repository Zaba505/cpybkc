      *****************************************************************
      * One COMP-6 item of four digits, whose second occurrence in the file
      * ends in a nibble no digit has.
      *
      * Two bytes. C is a valid sign nibble in a COMP-3 field and there is no
      * sign nibble in a COMP-6 one, so every value of A to F is a digit
      * position holding something that is not a digit.
      *****************************************************************
       01  COMP6-RECORD.
           05  C6-VALUE                PIC 9(4) COMP-6.
