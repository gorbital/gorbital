apistock — logo files
=====================

THE MARK
A lift of milled stock seen end-on: four bars, the top one pulled clear
and struck in accent — the board being taken to the bench.
Grid 14 units wide, bar 14 x 3, gap 2/3, top bar offset +2.6 right.
Square corners, 0 radius. Clear space = one bar height on all sides.

FILES
apistock-mark.svg                 primary, transparent, brand colours
apistock-mark-light.svg           for light grounds (accent dropped — lime fails contrast on white)
apistock-mark-mono.svg            single colour, inherits currentColor
apistock-lockup-dark.svg          mark + wordmark on #0c0c0a
apistock-lockup-light.svg         mark + wordmark on #fbfaf6
apistock-lockup-transparent.svg   mark + wordmark, no ground
apistock-avatar-lime.svg          square tile, lime ground (GitHub org, social)
apistock-avatar-dark.svg          square tile, dark ground
apistock-favicon.svg              3-bar reduction for 16-31px
*.png                             1024px rasters of the mark and tile

COLOURS
accent  #d8ff3e     ink   #f0efe9     ground #0c0c0a
grey-1  #6b6b63     grey-2 #3a3a34
light ground #fbfaf6, light ink #14140f

BEFORE HANDOFF
The lockups set the wordmark in Space Grotesk Bold (letter-spacing -0.05em).
Outline the text to paths before shipping anywhere the font isn't loaded.

NEVER
Round the corners. Add perspective, bevel or wood texture.
Make more than one bar accent. Offset the top bar left, or align it flush.
Tint the wordmark accent. Use lime as a text colour on light grounds.

RASTERS
png/  transparent where it makes sense; mark at 2048/1024/512, avatars at
      1024/512/256, favicon at 256/64/32/16, lockups at 2x and 1x.
jpg/  flattened onto a ground (JPG has no transparency): mark on dark and
      on light, avatars, and the three lockups.

Lockup rasters were captured with Space Grotesk Bold live, so they are
accurate. For vector handoff use the SVGs and outline the wordmark.
