using StageManager.Interop;

namespace StageManager.Services;

/// <summary>
/// A soft shadow in device pixels: a tight ambient glow around the window plus
/// a wide key shadow cast away from it. Each layer is a blurred copy of the
/// window's outline, like the shadows DWM draws. The key outline is first
/// shrunk by <see cref="KeyInset"/> on every side, so a dropped key shadow
/// reaches far below a window but hardly past its sides.
/// </summary>
internal readonly record struct ShadowStyle(
    float AmbientOpacity,
    float AmbientSigma,
    float KeyOpacity,
    float KeySigma,
    float KeyOffsetX,
    float KeyOffsetY,
    float KeyInset = 0)
{
    /// <summary>
    /// The shadow DWM draws under a Windows 11 window, fitted to captures of
    /// windows against the bare wallpaper, measured from their extended frame
    /// bounds: within 0.005 alpha on every side. A window whose frame is drawn
    /// active casts a darker one than an inactive window. Any difference shows
    /// as the shadow jumping when a flight hands over to the real window.
    /// </summary>
    internal static ShadowStyle Window(double deviceScale, bool isActive) => isActive
        ? new(
            0.176f,
            (float)(14.2 * deviceScale),
            0.235f,
            (float)(20.1 * deviceScale),
            0,
            (float)(37.9 * deviceScale),
            (float)(5.7 * deviceScale))
        : new(
            0.119f,
            (float)(7.0 * deviceScale),
            0.164f,
            (float)(21.0 * deviceScale),
            0,
            (float)(38.1 * deviceScale),
            (float)(7.6 * deviceScale));

    /// <summary>How far the shadow reaches beyond the window.</summary>
    internal float Extent => Math.Max(
        3 * AmbientSigma,
        3 * KeySigma + Math.Max(Math.Abs(KeyOffsetX), Math.Abs(KeyOffsetY)) - KeyInset);

    internal static ShadowStyle Lerp(ShadowStyle from, ShadowStyle to, float progress) => new(
        from.AmbientOpacity + (to.AmbientOpacity - from.AmbientOpacity) * progress,
        from.AmbientSigma + (to.AmbientSigma - from.AmbientSigma) * progress,
        from.KeyOpacity + (to.KeyOpacity - from.KeyOpacity) * progress,
        from.KeySigma + (to.KeySigma - from.KeySigma) * progress,
        from.KeyOffsetX + (to.KeyOffsetX - from.KeyOffsetX) * progress,
        from.KeyOffsetY + (to.KeyOffsetY - from.KeyOffsetY) * progress,
        from.KeyInset + (to.KeyInset - from.KeyInset) * progress);
}

/// <summary>
/// The shadow under one flight, drawn as DWM thumbnails of a <see cref="ShadowSource"/>.
/// DWM thumbnails leave out the shadow of their source window, so without it
/// a window would gain its shadow only once it landed.
/// </summary>
internal sealed class FlightShadow : IDisposable
{
    private readonly Layer _ambient;
    private readonly Layer _key;

    private FlightShadow(Layer ambient, Layer key)
    {
        _ambient = ambient;
        _key = key;
    }

    /// <summary>Register it right before its window's thumbnails so it lies directly beneath them.</summary>
    internal static FlightShadow? TryCreate(nint destination, nint source)
    {
        if (Layer.TryCreate(destination, source) is not { } ambient)
        {
            return null;
        }

        if (Layer.TryCreate(destination, source) is not { } key)
        {
            ambient.Dispose();
            return null;
        }

        return new FlightShadow(ambient, key);
    }

    /// <summary>Draws the shadow of a window drawn like <see cref="SlicedThumbnail.Show"/>.</summary>
    internal void Show(
        double left,
        double top,
        double width,
        double height,
        double tilt,
        double vanishingY,
        ShadowStyle style,
        double opacity)
    {
        _ambient.Show(left, top, width, height, tilt, vanishingY, style.AmbientSigma, 0, 0, 0, style.AmbientOpacity * opacity);
        _key.Show(left, top, width, height, tilt, vanishingY, style.KeySigma, style.KeyOffsetX, style.KeyOffsetY, style.KeyInset, style.KeyOpacity * opacity);
    }

    public void Dispose()
    {
        _ambient.Dispose();
        _key.Dispose();
    }

    /// <summary>
    /// One blurred outline, as a frame of pieces around the window. The middle
    /// is left out because the window covers it. Top and bottom are split into
    /// segments so they can follow the slanted edges of a turned window.
    /// </summary>
    private sealed class Layer : IDisposable
    {
        // Steps between segments must stay well inside the blur: a fully
        // turned card's edge drops up to ~30 px, against a ~16 px blur. Fewer
        // segments suffice for a gentler slant, and each one DWM has to resize
        // costs frame rate, see SlicedThumbnail.
        private const int EdgeSegments = 8;
        private const double SigmasPerStep = 0.25;
        private const int TopLeft = 0;
        private const int TopRight = 1;
        private const int BottomLeft = 2;
        private const int BottomRight = 3;
        private const int LeftEdge = 4;
        private const int RightEdge = 5;
        private const int TopEdges = 6;
        private const int BottomEdges = TopEdges + EdgeSegments;
        private const int PieceCount = BottomEdges + EdgeSegments;

        private readonly DwmThumbnail[] _pieces;

        private Layer(DwmThumbnail[] pieces)
        {
            _pieces = pieces;
        }

        internal static Layer? TryCreate(nint destination, nint source)
        {
            var pieces = new DwmThumbnail[PieceCount];
            for (var index = 0; index < PieceCount; index++)
            {
                if (DwmThumbnail.TryCreate(destination, source) is not { } piece)
                {
                    foreach (var created in pieces.Take(index))
                    {
                        created.Dispose();
                    }

                    return null;
                }

                pieces[index] = piece;
            }

            return new Layer(pieces);
        }

        internal void Show(
            double left,
            double top,
            double width,
            double height,
            double tilt,
            double vanishingY,
            double sigma,
            double offsetX,
            double offsetY,
            double shrink,
            double opacity)
        {
            left += shrink;
            top += shrink;
            width -= 2 * shrink;
            height -= 2 * shrink;
            if (opacity <= 0.004 || sigma < 0.5 || width < 1 || height < 1)
            {
                SetVisible(false);
                return;
            }

            var corners = Perspective.Corners(left, top, width, height, tilt, vanishingY);
            var near = left + offsetX;
            var far = near + width;
            var nearTop = corners[0].Y + offsetY;
            var farTop = corners[1].Y + offsetY;
            var farBottom = corners[2].Y + offsetY;
            var nearBottom = corners[3].Y + offsetY;

            // Pieces reach as far inward as outward, unless the window is too
            // small for opposite edges to stay apart.
            var reach = 3 * sigma;
            var inset = Math.Min(reach, Math.Min(width, farBottom - farTop) / 2);
            const int size = ShadowSource.Size;
            const int middle = ShadowSource.Middle;
            var span = (int)Math.Round(ShadowSource.Reach + inset * ShadowSource.Sigma / sigma);
            // Each corner is rounded from its own position, so it keeps its size
            // while the window grows; the pieces between corners take their
            // edges from the corners, so neighbours stay seamless.
            var topLeft = new PixelGrid(near, nearTop);
            var topRight = new PixelGrid(far, farTop);
            var bottomLeft = new PixelGrid(near, nearBottom);
            var bottomRight = new PixelGrid(far, farBottom);

            Place(TopLeft, topLeft.X(near - reach), topLeft.Y(nearTop - reach), topLeft.X(near + inset), topLeft.Y(nearTop + inset), 0, 0, span, span, opacity);
            Place(TopRight, topRight.X(far - inset), topRight.Y(farTop - reach), topRight.X(far + reach), topRight.Y(farTop + inset), size - span, 0, size, span, opacity);
            Place(BottomLeft, bottomLeft.X(near - reach), bottomLeft.Y(nearBottom - inset), bottomLeft.X(near + inset), bottomLeft.Y(nearBottom + reach), 0, size - span, span, size, opacity);
            Place(BottomRight, bottomRight.X(far - inset), bottomRight.Y(farBottom - inset), bottomRight.X(far + reach), bottomRight.Y(farBottom + reach), size - span, size - span, size, size, opacity);
            Place(LeftEdge, topLeft.X(near - reach), topLeft.Y(nearTop + inset), topLeft.X(near + inset), bottomLeft.Y(nearBottom - inset), 0, middle, span, middle + 1, opacity);
            Place(RightEdge, topRight.X(far - inset), topRight.Y(farTop + inset), topRight.X(far + reach), bottomRight.Y(farBottom - inset), size - span, middle, size, middle + 1, opacity);

            var slant = Math.Max(Math.Abs(farTop - nearTop), Math.Abs(farBottom - nearBottom));
            var segments = tilt < 0.005
                ? 1
                : Math.Clamp((int)Math.Ceiling(slant / Math.Max(1, sigma * SigmasPerStep)), 1, EdgeSegments);
            var start = near + inset;
            var length = width - 2 * inset;
            for (var index = 0; index < EdgeSegments; index++)
            {
                if (index >= segments)
                {
                    _pieces[TopEdges + index].SetVisible(false);
                    _pieces[BottomEdges + index].SetVisible(false);
                    continue;
                }

                var segmentLeft = start + length * index / segments;
                var segmentRight = start + length * (index + 1) / segments;
                var along = (segmentLeft + segmentRight) / 2 - near;
                var edgeTop = nearTop + (farTop - nearTop) * along / width;
                var edgeBottom = nearBottom + (farBottom - nearBottom) * along / width;
                var segmentStart = index == 0 ? topLeft.X(start) : topLeft.X(segmentLeft);
                var segmentEnd = index == segments - 1 ? topRight.X(far - inset) : topLeft.X(segmentRight);
                var topEdge = new PixelGrid(near, edgeTop);
                var bottomEdge = new PixelGrid(near, edgeBottom);
                Place(TopEdges + index, segmentStart, topEdge.Y(edgeTop - reach), segmentEnd, topEdge.Y(edgeTop + inset), middle, 0, middle + 1, span, opacity);
                Place(BottomEdges + index, segmentStart, bottomEdge.Y(edgeBottom - inset), segmentEnd, bottomEdge.Y(edgeBottom + reach), middle, size - span, middle + 1, size, opacity);
            }
        }

        public void Dispose()
        {
            foreach (var piece in _pieces)
            {
                piece.Dispose();
            }
        }

        private void SetVisible(bool visible)
        {
            foreach (var piece in _pieces)
            {
                piece.SetVisible(visible);
            }
        }

        private void Place(
            int piece,
            int left,
            int top,
            int right,
            int bottom,
            int sourceLeft,
            int sourceTop,
            int sourceRight,
            int sourceBottom,
            double opacity) =>
            _pieces[piece].ShowRegion(
                new NativeMethods.Rect
                {
                    Left = left,
                    Top = top,
                    Right = right,
                    Bottom = bottom
                },
                new NativeMethods.Rect
                {
                    Left = sourceLeft,
                    Top = sourceTop,
                    Right = sourceRight,
                    Bottom = sourceBottom
                },
                opacity);

        /// <summary>
        /// Rounds to device pixels relative to a rounded origin, so pieces that
        /// only move keep their sizes.
        /// </summary>
        private readonly record struct PixelGrid(double OriginX, double OriginY)
        {
            internal int X(double x) => (int)(Math.Round(OriginX) + Math.Round(x - OriginX));

            internal int Y(double y) => (int)(Math.Round(OriginY) + Math.Round(y - OriginY));
        }
    }
}
