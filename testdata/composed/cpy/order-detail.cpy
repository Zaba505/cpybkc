000010*  An order's detail record: the shared header, then the detail
000020*  body, which copies the shop's standard trailer.
000030 01  ORDER-DETAIL-REC.
000040     COPY STDHDR.
000050     COPY ORDDTL.
