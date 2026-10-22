using StageManager.Interop;

namespace StageManager.Services;

/// <summary>
/// A window preview drawn as vertical DWM thumbnail slices, so it can be shown
/// turned in perspective. When flat it uses a single slice. Rounded corners are
/// drawn by extra, narrower slices along both sides, cropped top and bottom.
/// </summary>
/// <remarks>
/// DWM pays for every thumbnail whose size changes in a frame, whatever its size
/// or source; moving one is free. Measured at 120 Hz: two dozen resizing at once
/// cost a quarter of the frames and fifty cost half. A preview therefore uses only
/// as many slices as its slant needs, and rounds its slices so that moving it
/// keeps their sizes.
/// </remarks>
internal sealed class SlicedThumbnail : IDisposable
{
    // A slice drawn flat shifts the columns inside it by up to this much against
    // where perspective puts them, in device pixels.
    private const double MaximumColumnShift = 0.5;

    private readonly nint _source;
    private readonly double _maximumStep;
    private readonly DwmThumbnail[] _slices;
    // The near side's corner slices, then the far side's, each from the outer edge in.
    private readonly DwmThumbnail[] _corners;
    // DWM keeps a source rectangle once one has been set: leaving it out later
    // does not mean the whole window again, so a slice that was turned would
    // keep showing its narrow strip stretched over the flat preview. The flat,
    // uncropped preview therefore has a thumbnail that never gets a source.
    private readonly DwmThumbnail _whole;
    private System.Drawing.Point _frameOrigin;

    private SlicedThumbnail(nint source, double maximumStep, DwmThumbnail whole, DwmThumbnail[] slices, DwmThumbnail[] corners)
    {
        _source = source;
        _maximumStep = maximumStep;
        _whole = whole;
        _slices = slices;
        _corners = corners;
        _frameOrigin = WindowGeometry.GetFrameOrigin(source) ?? default;
    }

    /// <summary>
    /// Registers every slice up front: thumbnails registered later draw on top,
    /// so a preview must occupy one contiguous block of the z-order.
    /// </summary>
    /// <param name="maximumStep">
    /// How far, in device pixels, the top or bottom edge may step between
    /// neighbouring slices. Larger steps need fewer slices.
    /// </param>
    /// <param name="cornerSlices">How many slices round each corner; none leaves the preview square.</param>
    internal static SlicedThumbnail? TryCreate(
        nint destination,
        nint source,
        int sliceCount,
        double maximumStep,
        int cornerSlices = 0)
    {
        if (DwmThumbnail.TryCreate(destination, source) is not { } whole)
        {
            return null;
        }

        var slices = new List<DwmThumbnail>(sliceCount);
        for (var index = 0; index < sliceCount; index++)
        {
            if (DwmThumbnail.TryCreate(destination, source) is not { } slice)
            {
                break;
            }

            slices.Add(slice);
        }

        if (slices.Count == 0)
        {
            whole.Dispose();
            return null;
        }

        var corners = new List<DwmThumbnail>(cornerSlices * 2);
        for (var index = 0; index < cornerSlices * 2; index++)
        {
            if (DwmThumbnail.TryCreate(destination, source) is not { } corner)
            {
                // Corners are all or nothing; without them the preview is square.
                corners.ForEach(static thumbnail => thumbnail.Dispose());
                corners.Clear();
                break;
            }

            corners.Add(corner);
        }

        return new SlicedThumbnail(source, maximumStep, whole, [.. slices], [.. corners]);
    }

    /// <summary>
    /// Draws the cropped window into the given device-pixel rectangle, turned
    /// by <paramref name="tilt"/> with its far edge leaning toward <paramref name="vanishingY"/>,
    /// its corners rounded by <paramref name="cornerRadius"/> device pixels at the near edge.
    /// </summary>
    internal void Show(
        double left,
        double top,
        double width,
        double height,
        ThumbnailCrop crop,
        double tilt,
        double vanishingY,
        double opacity,
        NativeMethods.Rect? clip = null,
        double cornerRadius = 0)
    {
        if (width < 1 || height < 1 || opacity <= 0.004 || !_slices[0].TryGetSourceSize(out var sourceSize))
        {
            SetVisible(false);
            return;
        }

        // A minimized window keeps the frame it was measured with.
        _frameOrigin = WindowGeometry.GetFrameOrigin(_source) ?? _frameOrigin;
        var source = crop.Apply(sourceSize);
        var count = tilt < 0.005 ? 1 : CountSlices(top, width, height, tilt, vanishingY);
        var isRounded = cornerRadius >= 1 && _corners.Length > 0;
        // Everything is placed relative to the rounded corner, so a preview that
        // only moves keeps the size of every slice.
        var originX = Math.Round(left);
        var originY = Math.Round(top);
        // A flat, uncropped preview asks for no source rectangle at all: DWM reads
        // an explicit one from the window's corner, which would shift the frame.
        var whole = source.Left == 0 && source.Top == 0 &&
                    source.Right == sourceSize.Width && source.Bottom == sourceSize.Height;
        if (count == 1 && whole && !isRounded)
        {
            // Show the new picture before hiding the old one, so no frame lacks both.
            _whole.ShowRegion(
                new NativeMethods.Rect
                {
                    Left = (int)originX,
                    Right = (int)(originX + Math.Round(width)),
                    Top = (int)originY,
                    Bottom = (int)(originY + Math.Round(height))
                },
                null,
                opacity,
                clip,
                _frameOrigin);
            foreach (var slice in _slices.Concat(_corners))
            {
                slice.SetVisible(false);
            }

            return;
        }

        var centerY = top + height / 2;

        // Draws the window's columns between from and to (0 = near edge, 1 = far
        // edge), leaving out inset, a fraction of its height, at top and bottom.
        void ShowColumn(DwmThumbnail slice, double from, double to, double inset)
        {
            var near = Perspective.ScreenPosition(from, tilt);
            var far = Perspective.ScreenPosition(to, tilt);
            var scale = count == 1 ? 1 : Perspective.Scale((near + far) / 2, tilt);
            var sliceCenter = Perspective.CenterY(centerY, vanishingY, scale);
            var halfHeight = height * scale * (0.5 - inset);
            slice.ShowRegion(
                new NativeMethods.Rect
                {
                    // Shared, rounded edges keep neighbouring slices seamless.
                    Left = (int)(originX + Math.Round(width * near)),
                    Right = (int)(originX + Math.Round(width * far)),
                    Top = (int)(originY + Math.Round(sliceCenter - halfHeight - top)),
                    Bottom = (int)(originY + Math.Round(sliceCenter + halfHeight - top))
                },
                new NativeMethods.Rect
                {
                    Left = _frameOrigin.X + source.Left + (int)Math.Round(source.Width * from),
                    Right = _frameOrigin.X + source.Left + (int)Math.Round(source.Width * to),
                    Top = _frameOrigin.Y + source.Top + (int)Math.Round(source.Height * inset),
                    Bottom = _frameOrigin.Y + source.Bottom - (int)Math.Round(source.Height * inset)
                },
                opacity,
                clip);
        }

        // The corners are round on the window itself, so the far ones come out
        // narrower and shorter, like the rest of the far edge.
        var cornerWidth = 0.0;
        var cornerHeight = 0.0;
        if (isRounded)
        {
            var radius = Math.Min(cornerRadius, Math.Min(width, height) / 2);
            cornerWidth = Math.Min(Perspective.SourcePosition(radius / width, tilt), 0.5);
            cornerHeight = radius / height;
        }

        for (var index = 0; index < _slices.Length; index++)
        {
            if (index >= count)
            {
                _slices[index].SetVisible(false);
                continue;
            }

            // Slices take equal parts of the window; nearer ones are drawn wider.
            var span = 1 - 2 * cornerWidth;
            ShowColumn(
                _slices[index],
                cornerWidth + span * index / count,
                cornerWidth + span * (index + 1) / count,
                0);
        }

        var perSide = _corners.Length / 2;
        for (var index = 0; index < perSide; index++)
        {
            if (!isRounded)
            {
                _corners[index].SetVisible(false);
                _corners[perSide + index].SetVisible(false);
                continue;
            }

            // Equal steps around the quarter circle, from the outer edge in.
            var start = Math.PI / 2 * index / perSide;
            var end = Math.PI / 2 * (index + 1) / perSide;
            var outer = cornerWidth * (1 - Math.Cos(start));
            var inner = cornerWidth * (1 - Math.Cos(end));
            var inset = cornerHeight * (1 - Math.Sin((start + end) / 2));
            ShowColumn(_corners[index], outer, inner, inset);
            ShowColumn(_corners[perSide + index], 1 - inner, 1 - outer, inset);
        }

        _whole.SetVisible(false);
    }

    internal void SetVisible(bool visible)
    {
        // Only Show knows which of the two pictures belongs on screen.
        if (!visible)
        {
            _whole.SetVisible(false);
        }

        foreach (var slice in _slices.Concat(_corners))
        {
            slice.SetVisible(visible);
        }
    }

    /// <summary>The size DWM reports for the source window, the space crops are measured in.</summary>
    internal bool TryGetSourceSize(out NativeMethods.Size size)
    {
        if (_slices.Length > 0)
        {
            return _slices[0].TryGetSourceSize(out size);
        }

        size = default;
        return false;
    }

    public void Dispose()
    {
        _whole.Dispose();
        foreach (var slice in _slices.Concat(_corners))
        {
            slice.Dispose();
        }
    }

    /// <summary>
    /// Enough slices that the slanted edges step by no more than the maximum step,
    /// and that drawing each slice flat moves no column by more than
    /// <see cref="MaximumColumnShift"/>.
    /// </summary>
    private int CountSlices(double top, double width, double height, double tilt, double vanishingY)
    {
        if (_maximumStep <= 0)
        {
            return _slices.Length;
        }

        var corners = Perspective.Corners(0, top, width, height, tilt, vanishingY);
        var slant = Math.Max(Math.Abs(corners[1].Y - corners[0].Y), Math.Abs(corners[2].Y - corners[3].Y));
        // Flat slices follow the perspective curve in straight pieces; the error
        // of a piece shrinks with the square of the number of pieces.
        var curve = (1 - Perspective.Scale(1, tilt)) * width / 4;
        var count = Math.Max(slant / _maximumStep, Math.Sqrt(curve / MaximumColumnShift));
        return Math.Clamp((int)Math.Ceiling(count), 1, _slices.Length);
    }
}
