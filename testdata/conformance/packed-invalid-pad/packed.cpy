      *****************************************************************
      * One packed item of four digits, whose second occurrence in the file
      * carries a non-zero pad nibble.
      *
      * Three bytes: four digits and a sign is five nibbles, which rounds up to
      * three bytes and leaves one pad nibble at the front.
      *****************************************************************
       01  PACKED-RECORD.
           05  P-VALUE                 PIC S9(4) COMP-3.
